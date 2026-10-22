package server

import (
	"strings"
	"time"
)

// scannerPathHints 自动化扫描器常用的路径片段。公网上的 .svn/.git、备份包、
// 配置文件探测会以极高频率命中 NoRoute；不拦下来会把 api_probe 的 1000 条
// 上限刷满，真正需要补的客户端端点反而在列表里看不见。
var scannerPathHints = []string{
	"wp-admin", "wp-login", "wp-content", "wp-includes", "wordpress",
	"phpmyadmin", "phpinfo", "xmlrpc", "cgi-bin", "vendor/phpunit", "webshell",
}

// isScannerProbe 判断 NoRoute 命中的路径是否明显来自自动化扫描器。
//
// 判据只取「Emby 兼容客户端绝不会发」的形状，宁可漏拦也不误伤真实端点：
//   - 路径任一段以 '.' 开头（长度 > 1）：隐藏文件/目录，如 /.svn/entries、
//     /old/.svn/entries、/.git-credentials、/.env；
//   - 末段含 '.'：带扩展名的文件探测，如 /secrets.yml、/emby-115-cf.7z、
//     /web.config、/database.yaml；
//   - 命中常见扫描词。
//
// Emby API 的路径都是无扩展名的驼峰段（/Users/1/Views、/Branding/Configuration、
// /Persons），三条判据都不会命中；而 /Videos/:id/stream.mp4 这类带扩展名的
// 是已注册路由，根本不走 NoRoute。
func isScannerProbe(path string) bool {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return false
	}
	segments := strings.Split(trimmed, "/")
	if strings.Contains(segments[len(segments)-1], ".") {
		return true
	}
	for _, seg := range segments {
		if len(seg) > 1 && seg[0] == '.' {
			return true
		}
	}
	lower := strings.ToLower(trimmed)
	for _, hint := range scannerPathHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// 诊断去重与写库预算参数（BE-08）：128 条内存上限、6 小时去重窗口、
// 令牌桶 60 次突发 + 平均每秒 1 次落库，避免唯一路径洪水把写放大成
// 每秒数百次 SQLite 事务。
const (
	probeSeenLimit      = 128
	probeSeenTTL        = 6 * time.Hour
	probeWriteBurst     = 60.0
	probeWritePerSecond = 1.0
)

type probeSeenEntry struct {
	seenAt time.Time
}

func (a *App) probeClock() time.Time {
	if a.probeNow != nil {
		return a.probeNow()
	}
	return time.Now()
}

// observeProbe 在 probeMu 内完成去重、TTL 过期、容量淘汰与写库预算扣减。
// 返回是否应落库，以及预约时的 epoch（clear 提交后 epoch 变化则放弃写入）。
// 预算耗尽时不占位：后续请求会在令牌恢复后重试，避免把端点永久标记为已见。
func (a *App) observeProbe(key string, now time.Time) (bool, uint64) {
	a.probeMu.Lock()
	defer a.probeMu.Unlock()
	if entry, ok := a.probeSeen[key]; ok {
		if now.Sub(entry.seenAt) < probeSeenTTL {
			return false, a.probeEpoch
		}
		delete(a.probeSeen, key)
	}
	if len(a.probeSeen) >= probeSeenLimit {
		a.evictProbeSeenLocked(now)
	}
	if !a.takeProbeWriteTokenLocked(now) {
		return false, a.probeEpoch
	}
	a.probeSeen[key] = probeSeenEntry{seenAt: now}
	return true, a.probeEpoch
}

// evictProbeSeenLocked 先清过期项，仍满则淘汰最久未出现的键（LRU）。
// 旧实现容量满后把所有新端点都当已见，导致新端点永远进不了诊断列表。
func (a *App) evictProbeSeenLocked(now time.Time) {
	var oldestKey string
	var oldest time.Time
	for key, entry := range a.probeSeen {
		if now.Sub(entry.seenAt) >= probeSeenTTL {
			delete(a.probeSeen, key)
			continue
		}
		if oldestKey == "" || entry.seenAt.Before(oldest) {
			oldestKey, oldest = key, entry.seenAt
		}
	}
	if len(a.probeSeen) >= probeSeenLimit && oldestKey != "" {
		delete(a.probeSeen, oldestKey)
	}
}

func (a *App) takeProbeWriteTokenLocked(now time.Time) bool {
	if a.probeRefillAt.IsZero() {
		a.probeTokens, a.probeRefillAt = probeWriteBurst, now
	}
	if elapsed := now.Sub(a.probeRefillAt); elapsed > 0 {
		a.probeTokens += elapsed.Seconds() * probeWritePerSecond
		if a.probeTokens > probeWriteBurst {
			a.probeTokens = probeWriteBurst
		}
		a.probeRefillAt = now
	}
	if a.probeTokens < 1 {
		return false
	}
	a.probeTokens--
	return true
}

// Lock order: persistMu -> probeMu. Reservation in noRoute releases probeMu
// before waiting for persistMu; duplicate requests never wait for SQLite.
func (a *App) persistDiagnosticProbe(method, path, query string, epoch uint64) {
	a.probePersistMu.Lock()
	defer a.probePersistMu.Unlock()
	a.probeMu.Lock()
	current := epoch == a.probeEpoch
	a.probeMu.Unlock()
	if !current {
		// A clear committed after this request reserved its slot. Neither write the
		// old observation nor delete a replacement reservation from the new epoch.
		return
	}
	if err := a.db.Probe(method, path, query, "[OMITTED]"); err != nil {
		a.probeMu.Lock()
		delete(a.probeSeen, method+" "+path)
		a.probeMu.Unlock()
	}
}

func (a *App) clearDiagnosticProbes() error {
	a.probePersistMu.Lock()
	defer a.probePersistMu.Unlock()
	if err := a.db.ClearProbes(); err != nil {
		return err
	}
	a.probeMu.Lock()
	a.probeSeen = make(map[string]probeSeenEntry)
	a.probeEpoch++
	a.probeMu.Unlock()
	return nil
}
