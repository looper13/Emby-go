<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { configLabels, restartKeys, type ConfigPatch, type ConfigState } from '../../api/system-config';
const props = defineProps<{ state: ConfigState; saving: boolean }>();
const emit = defineEmits<{ save: [patch: ConfigPatch]; dirty: [dirty: boolean]; reload: [] }>();
const clone = () => ({ ...props.state.config, server_domains: props.state.config.server_domains.map(d => ({ ...d })) });
const draft = ref(clone());
const password = ref(''), clearPassword = ref(false);
const alistToken = ref(''), clearAListToken = ref(false);
function restore() { draft.value = clone(); password.value = ''; clearPassword.value = false; alistToken.value = ''; clearAListToken.value = false; }
watch(() => props.state, restore);
const patch = computed(() => {
  const values: Record<string, unknown> = {};
  for (const key of Object.keys(draft.value) as (keyof typeof draft.value)[]) {
    if (key !== 'redis_password_configured' && key !== 'alist_token_configured' && JSON.stringify(draft.value[key]) !== JSON.stringify(props.state.config[key])) values[key] = draft.value[key];
  }
  if (clearPassword.value) values.clear_redis_password = true;
  else if (password.value) values.redis_password = password.value;
  if (clearAListToken.value) values.clear_alist_token = true;
  else if (alistToken.value) values.alist_token = alistToken.value;
  return values as ConfigPatch;
});
const dirty = computed(() => Object.keys(patch.value).length > 0);
const restartPending = computed(() => props.state.restart_fields.map(k => configLabels[k] || k).join('、'));
watch(dirty, value => emit('dirty', value), { immediate: true });
const restartEdits = computed(() => Object.keys(patch.value).filter(k => restartKeys.includes(k) || k === 'clear_redis_password')
  .map(k => configLabels[k === 'clear_redis_password' ? 'redis_password' : k]).join('、'));
function submit() { if (dirty.value && !props.saving && props.state.writable) emit('save', patch.value); }
</script>

<template>
  <form id="system-config-form" class="config-form" @submit.prevent="submit">
    <section class="panel config-intro">
      <div class="panel-head"><div><h2>实例配置</h2><p class="hint">管理服务连接、存储与媒体探测。修改后点击保存。</p></div><span class="config-tag">YAML 配置</span></div>
      <div class="config-source"><span>配置文件</span><code data-private-path>{{ state.config_path || '未指定配置文件' }}</code></div>
      <p v-if="!state.writable" class="config-notice" role="alert">当前实例未指定配置文件。请使用 -c 指定配置文件后启动，以启用页面保存。</p>
      <p v-if="state.restart_fields.length" class="config-notice" role="status">已保存，等待重启：{{ restartPending }}。当前服务继续使用原值。</p>
      <p v-for="warning in state.warnings" :key="warning" class="auth-error" role="alert">配置已保存；{{ warning }}</p>
    </section>

    <fieldset :disabled="saving || !state.writable" class="config-fieldset">
      <section class="panel">
        <div class="panel-head"><h3>服务基础</h3><span class="config-tag live">名称即时生效</span></div>
        <div class="field-grid">
          <div class="field"><label for="cfg-name">服务名称</label><input id="cfg-name" v-model="draft.server_name" required maxlength="200"><small class="hint">显示在 Emby 客户端中。</small></div>
          <div class="field"><label for="cfg-port">监听端口 <span class="config-restart">重启生效</span></label><input id="cfg-port" v-model.number="draft.port" type="number" min="1" max="65535" required><small class="hint">当前端口：{{ state.effective.port }}。重启后请使用新端口访问。</small></div>
          <div class="field"><label class="check"><input id="cfg-debug" v-model="draft.debug" type="checkbox">启用调试模式 <span class="config-restart">重启生效</span></label><small class="hint">记录详细的脱敏请求头日志。</small></div>
          <div class="field"><label for="cfg-id">实例标识（只读）</label><input id="cfg-id" :value="state.server_id" readonly class="mono"><small class="hint">客户端使用此标识识别同一实例。</small></div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-head"><h3>媒体库监控</h3><span class="config-tag live">保存后生效</span></div>
        <div class="field-grid">
          <div class="field"><label class="check"><input id="cfg-monitor" :checked="!draft.disable_library_monitor" type="checkbox" @change="draft.disable_library_monitor = !($event.target as HTMLInputElement).checked">启用媒体库监控</label><small class="hint">文件变化时自动更新索引。关闭后仍可手动或定时扫描。</small></div>
          <div class="field"><label for="cfg-mode">监控模式</label><select id="cfg-mode" v-model="draft.library_monitor_mode"><option value="realtime">实时监听</option><option value="polling">兼容模式（每 30 秒检查）</option></select><small class="hint">网络挂载无法收到文件事件时可使用兼容模式。</small></div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-head"><h3>存储目录</h3><span class="config-tag">重启生效</span></div>
        <div class="field-grid">
          <div class="field full"><label for="cfg-db">数据库路径</label><input id="cfg-db" v-model="draft.db_path" required class="mono" data-private-path><small class="hint">相对路径基于进程启动目录。切换路径将使用目标数据库，不会搬迁数据。</small><small class="hint" data-private-path>当前使用：{{ state.effective.db_path }}</small></div>
          <div class="field full"><label for="cfg-actors">演员资料目录</label><input id="cfg-actors" v-model="draft.actors_dir" class="mono" placeholder="留空 = 数据库同级 actors/" data-private-path><small class="hint">相对路径基于数据库所在目录；已有头像和资料需要自行迁移。</small><small class="hint" data-private-path>当前使用：{{ state.effective.actors_dir }}</small></div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-head"><h3>Redis 缓存</h3><span class="config-tag">重启生效</span></div>
        <div class="field-grid">
          <div class="field"><label for="cfg-redis">连接地址</label><input id="cfg-redis" v-model="draft.redis_addr" required class="mono" placeholder="127.0.0.1:6379"><small class="hint">Redis 为必选缓存，启动时需要连接成功。</small></div>
          <div class="field"><label for="cfg-redis-db">数据库编号</label><input id="cfg-redis-db" v-model.number="draft.redis_db" type="number" min="0" required></div>
          <div class="field"><label for="cfg-password">连接密码</label><input id="cfg-password" v-model="password" :disabled="clearPassword" type="password" autocomplete="new-password" data-secret :placeholder="state.config.redis_password_configured ? '已配置（留空保持不变）' : '未配置（留空保持不变）'"></div>
          <div class="field"><label class="check"><input id="cfg-clear-password" v-model="clearPassword" type="checkbox">清除已保存的 Redis 密码</label><small class="hint">仅在 Redis 不再需要密码时勾选。</small></div>
        </div>
      </section>

      <section class="panel" id="alist-config">
        <div class="panel-head"><h3>AList 远程文件管理</h3><span class="config-tag live">无需重启</span></div>
        <p class="hint">用于“媒体库 → 影片查重 → 删除此副本”。地址须与 STRM 中 AList 链接的服务地址一致；Token 使用 AList 管理后台的 API Token，需要文件删除权限。</p>
        <div class="field-grid">
          <div class="field full"><label for="cfg-alist-url">AList 服务地址</label><input id="cfg-alist-url" v-model="draft.alist_url" type="url" class="mono" placeholder="http://192.168.1.4:5244"><small class="hint">支持 /d/、/p/、/dav/ 链接和反向代理子路径。已转成网盘直链的 STRM 无法定位；留空停用远程删除。</small></div>
          <div class="field"><label for="cfg-alist-token">AList API Token</label><input id="cfg-alist-token" v-model="alistToken" :disabled="clearAListToken" type="password" autocomplete="new-password" data-secret :placeholder="state.config.alist_token_configured ? '已配置（留空保持不变）' : '未配置'"></div>
          <div class="field"><label class="check"><input id="cfg-clear-alist-token" v-model="clearAListToken" type="checkbox">清除已保存的 AList Token</label><small class="hint">Token 仅保存在服务端；清除后使用 AList 访客权限。</small></div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-head"><h3>媒体信息探测</h3><span class="config-tag live">无需重启</span></div>
        <div class="field-grid">
          <div class="field full"><label for="cfg-ffprobe">ffprobe 路径</label><input id="cfg-ffprobe" v-model="draft.ffprobe_path" class="mono" placeholder="留空 = 从系统 PATH 查找" data-private-path><small class="hint">使用新路径启动下一次探测任务，开始前自动检查可执行文件。</small></div>
          <div class="field"><label for="cfg-timeout">单条探测超时（秒）</label><input id="cfg-timeout" v-model.number="draft.probe_timeout_seconds" type="number" min="1" max="3600" required><small class="hint">对后续发起的单条探测生效，默认 90 秒。</small></div>
          <div class="field"><label for="cfg-concurrency">并发数</label><input id="cfg-concurrency" v-model.number="draft.probe_concurrency" type="number" min="1" max="8" required><small class="hint">下一次批量任务使用，建议 2。</small></div>
        </div>
      </section>

      <section class="panel">
        <div class="panel-head"><h3>字幕与音轨</h3><span class="config-tag live">无需重启</span></div>
        <p class="hint">本地外挂字幕无需 FFmpeg。启用后可提取内封文本字幕，并在网页播放时重封装其他音轨；重封装的视频经过本服务，占用带宽与临时磁盘，视频编码保持原样。</p>
        <div class="field-grid">
          <div class="field full"><label class="check"><input id="cfg-ffmpeg-enabled" v-model="draft.ffmpeg_enabled" type="checkbox">启用 FFmpeg 播放处理</label></div>
          <div class="field full"><label for="cfg-ffmpeg">FFmpeg 路径</label><input id="cfg-ffmpeg" v-model="draft.ffmpeg_path" class="mono" placeholder="留空 = 从系统 PATH 查找" data-private-path></div>
          <div class="field"><label for="cfg-playback-concurrency">播放处理并发</label><input id="cfg-playback-concurrency" v-model.number="draft.playback_concurrency" type="number" min="1" max="4"><small class="hint">字幕提取与音轨处理共用，默认 2。已有会话不受修改影响。</small></div>
          <div class="field"><label for="cfg-playback-cache">每个播放会话缓存上限（MiB）</label><input id="cfg-playback-cache" v-model.number="draft.playback_cache_mb" type="number" min="64" max="65536"><small class="hint">默认 2048，达到上限时停止处理。高码率或长影片可调大；关闭播放后清理。</small></div>
        </div>
      </section>
      <section class="panel">
        <div class="panel-head"><h3>Emby 备用域名</h3><span class="config-tag live">即时生效</span></div>
        <p class="hint">向支持线路切换的客户端提供候选入口。</p>
        <div v-for="(domain, index) in draft.server_domains" :key="index" class="config-domain">
          <div class="field"><label :for="`cfg-domain-name-${index}`">线路 {{ index + 1 }} 名称</label><input :id="`cfg-domain-name-${index}`" v-model="domain.name" required maxlength="200" placeholder="如：内网"></div>
          <div class="field"><label :for="`cfg-domain-url-${index}`">HTTP / HTTPS 地址</label><input :id="`cfg-domain-url-${index}`" v-model="domain.url" type="url" required placeholder="https://emby.example.com/emby" class="mono"></div>
          <button type="button" class="btn" :aria-label="`删除线路 ${index + 1}`" @click="draft.server_domains.splice(index, 1)">删除</button>
        </div>
        <p v-if="!draft.server_domains.length" class="hint">尚未配置备用线路。</p>
        <button id="cfg-add-domain" type="button" class="btn" :disabled="draft.server_domains.length >= 32" @click="draft.server_domains.push({ name: '', url: '' })">添加线路</button>
      </section>
    </fieldset>

    <section class="panel config-actions">
      <p v-if="restartEdits" class="hint">本次修改中的 {{ restartEdits }} 将在重启后生效。</p>
      <div class="form-foot"><button id="cfg-save" type="submit" class="btn btn-accent" :disabled="saving || !dirty || !state.writable">{{ saving ? '正在保存…' : '保存配置' }}</button><button id="cfg-restore" type="button" class="btn" :disabled="saving || !dirty" @click="restore">恢复已保存设置</button><button id="cfg-reload" type="button" class="btn" :disabled="saving" @click="emit('reload')">重新加载文件</button><span id="cfg-dirty" class="hint" role="status">{{ dirty ? '有未保存的修改' : '与已保存配置一致' }}</span></div>
    </section>
  </form>
</template>

<style scoped>
.config-form, .config-fieldset { display: grid; gap: 20px; }
.config-fieldset { border: 0; padding: 0; margin: 0; min-width: 0; }
.config-form .panel { margin: 0; }
.config-form .field-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.config-form { margin-bottom: 20px; }
.config-form h3 { margin: 0; font-size: 16px; }
.config-form .hint { line-height: 1.65; }
.config-form .field { min-width: 0; }
.config-form small.hint { display: block; margin: 4px 0 0; overflow-wrap: anywhere; }
.config-form .panel-head { flex-wrap: wrap; gap: 12px; }
.config-tag { font-size: 11px; padding: 4px 9px; border: 1px solid var(--line); border-radius: 20px; color: var(--muted); white-space: nowrap; }
.config-tag.live { color: var(--accent); }
.config-restart { font-size: 11px; color: var(--muted); font-weight: normal; margin-left: 5px; }
.config-source { display: flex; gap: 16px; flex-wrap: wrap; font-size: 12px; }
.config-source code { overflow-wrap: anywhere; }
.config-notice { padding: 12px 14px; border-left: 3px solid var(--accent); background: var(--panel-2); font-size: 13px; line-height: 1.65; }
.config-domain { display: grid; grid-template-columns: minmax(100px, 1fr) minmax(180px, 2fr) auto; gap: 12px; align-items: end; margin-bottom: 14px; }
.config-actions .form-foot { margin: 0; flex-wrap: wrap; }
.config-actions .hint { margin: 0 0 10px; }
.config-actions #cfg-dirty { margin: 0; }
@media (max-width: 640px) { .config-domain { grid-template-columns: 1fr; } .config-form .field-grid { grid-template-columns: 1fr; } .config-domain > button { justify-self: start; } }
</style>
