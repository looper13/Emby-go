<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, shallowRef, watch } from 'vue';
import { ApiError } from '../../api/client';
import { duplicatesApi, duplicateRuleLabels, type DuplicateSelection, type DuplicateSelectionItem, type DuplicateDeletePreview } from '../../api/duplicates';
import { usePageRequest } from '../../composables/usePageRequest';

const props=defineProps<{selection:DuplicateSelection; libraryName:string}>();
const emit=defineEmits<{close:[completed:number]}>();
const shell=ref<HTMLDialogElement>(),closeButton=ref<HTMLButtonElement>(),list=ref<HTMLDivElement>();
type DeleteResult = { status:'success'|'failed'; error:string; removed:string[] };
const scope=usePageRequest(),running=ref(false),confirmed=ref(false),offset=ref(0),onlyFailed=ref(false),phase=ref<'idle'|'preview'|'delete'>('idle');
const stopRequested=ref(false),preview=shallowRef<DuplicateDeletePreview|null>(null),current=shallowRef<DuplicateSelectionItem|null>(null),pageSize=20;
const results=ref(new Map<number,DeleteResult>());
const pending=computed(()=>props.selection.items.filter(item=>!results.value.has(item.movie.id)));
const failed=computed(()=>props.selection.items.filter(item=>results.value.get(item.movie.id)?.status==='failed'));
const completed=computed(()=>results.value.size-failed.value.length);
const listed=computed(()=>onlyFailed.value?failed.value:props.selection.items);
const page=computed(()=>listed.value.slice(offset.value,offset.value+pageSize));
watch(()=>listed.value.length,total=>{offset.value=Math.min(offset.value,Math.max(0,Math.ceil(total/pageSize)-1)*pageSize);});
watch([onlyFailed,offset],()=>{if(list.value) list.value.scrollTop=0;},{flush:'post'});
let active=true;
function date(value:string) {return value && !Number.isNaN(Date.parse(value))?new Date(value).toLocaleString():'未知';}
function close() {if(!running.value) emit('close',completed.value);}
function stop() {stopRequested.value=true;if(phase.value==='preview') scope.cancel();}
function toggleFailures() {onlyFailed.value=!onlyFailed.value;offset.value=0;}
async function run() {
  if(running.value || !confirmed.value) return;
  // Snapshot one pass: failures are retried only after another confirmation.
  const queue=pending.value.length?pending.value:failed.value;
  if(!queue.length) return;
  running.value=true;confirmed.value=false;stopRequested.value=false;
  for(const item of queue) {
    if(!active || stopRequested.value) break;
    current.value=item;
    const proof={keep_id:item.keep.id,selection_token:item.selection_token};
    try {
      phase.value='preview';preview.value=null;
      const request=scope.begin();
      const checked=await duplicatesApi.previewDelete(item.movie.id,props.selection.library_id,item.key,request.signal,proof);
      if(!active || stopRequested.value || !request.isCurrent()) break;
      preview.value=checked;phase.value='delete';
      // Fresh per-copy previews avoid expiring tokens while a long queue runs.
      await duplicatesApi.deleteCopy(item.movie.id,props.selection.library_id,item.key,checked,proof);
      if(!active) break;
      results.value.set(item.movie.id,{status:'success',error:'',removed:[]});
    } catch(cause) {
      if(!active || (stopRequested.value && phase.value==='preview')) break;
      const error=(cause instanceof Error || cause instanceof DOMException) && cause.name==='TimeoutError'?'当前副本核验超时，请检查 STRM 与 AList 后重试。':cause instanceof Error || cause instanceof DOMException?cause.message || '副本删除失败':'副本删除失败';
      const removed=cause instanceof ApiError && cause.payload && typeof cause.payload==='object' && 'removed_remote_paths' in cause.payload && Array.isArray(cause.payload.removed_remote_paths)?cause.payload.removed_remote_paths.filter((value):value is string=>typeof value==='string'):[];
      results.value.set(item.movie.id,{status:'failed',error,removed});
    } finally {
      if(active) {current.value=null;preview.value=null;}
    }
  }
  if(active) {running.value=false;phase.value='idle';confirmed.value=false;}
}
onMounted(()=>{if(shell.value?.showModal) shell.value.showModal();else shell.value?.setAttribute('open','');closeButton.value?.focus();});
onScopeDispose(()=>{active=false;scope.cancel();shell.value?.close?.();});
</script>

<template>
  <dialog ref="shell" class="duplicate-delete-dialog duplicate-bulk-dialog" aria-labelledby="duplicate-bulk-title" @cancel.prevent="close">
    <div class="duplicate-delete-head"><h3 id="duplicate-bulk-title">批量删除多余副本</h3><button ref="closeButton" class="btn btn-sm" :disabled="running" @click="close">{{ !pending.length?'关闭':'取消 / 关闭' }}</button></div>
    <p><strong>{{ libraryName }}</strong> · {{ duplicateRuleLabels[selection.rule] }} · {{ selection.groups }} 组 · 全量选择 {{ selection.total }} 份</p>
    <p class="hint">待删清单覆盖全部结果页，只删除本次选中的副本。每组指定保留一份，执行时再次检查保留副本的索引和路径未变；不会读取保留副本的 STRM。加入时间是首次入库时间，路径长度按主文件完整路径的字符数计算；相同值保留 ID 较小的一份，未知时间排在有效时间后。</p>
    <p class="auth-error">将通过 AList 删除远程原文件，再清理本地 STRM（含 CD 分段）和影片索引，保留 NFO、封面及其他文件。请核对下方待删/保留路径，确认保留副本可播放。网盘删除不可跨系统回滚。</p>
    <p role="status">已处理 {{ results.size }} / {{ selection.total }} 份（成功 {{ completed }}，失败 {{ failed.length }}），未处理 {{ pending.length }} 份{{ running?`，正在${phase==='preview'?'核验':'删除'}当前副本`:!pending.length?failed.length?'，本轮处理结束，可仅重试失败项':'，批量删除完成':results.size || stopRequested?'，队列已停止，可确认后继续':'' }}</p>
    <template v-if="running && current">
      <p class="mono duplicate-path" data-private-path>{{ current.movie.source_path }}</p>
      <p v-for="file in preview?.files || []" :key="file.local_path" class="mono duplicate-path" data-private-path>远程：{{ file.remote_path }}</p>
      <button class="btn btn-sm" :disabled="stopRequested" @click="stop">{{ stopRequested?'正在停止，当前删除完成后停下':'停止队列' }}</button>
      <p class="hint">每次只处理一个副本。停止或离开页面不会继续下一份；已发出的删除会完成当前副本的清理记账。</p>
    </template>
    <p v-if="failed.length" class="auth-error" role="alert">{{ failed.length }} 份副本处理失败，已记录原因{{ running?'，继续处理本轮后续副本':'；已成功项不会再次处理' }}。失败项不计为删除成功，保留尚未删除的本地 STRM 与索引。网络响应丢失时请先重新查重确认当前状态；保留副本或路径变化需关闭后重新全量选择。</p>
    <div v-if="failed.length || onlyFailed" class="duplicate-pagination"><button id="duplicate-bulk-filter-failed" class="btn btn-sm" :aria-pressed="onlyFailed" @click="toggleFailures">{{ onlyFailed?'查看全部清单':`只看失败 ${failed.length} 份` }}</button></div>
    <div ref="list" class="duplicate-bulk-list" tabindex="0" aria-label="全量待删及保留副本清单">
      <p v-if="!page.length" class="hint">暂无失败副本。</p>
      <article v-for="item in page" :key="item.movie.id" :data-duplicate-result="item.movie.id" class="duplicate-bulk-item">
        <strong class="mono">{{ item.key }}</strong><span class="hint"> · {{ current?.movie.id===item.movie.id?phase==='preview'?'正在核验':'正在删除':results.get(item.movie.id)?.status==='success'?'已成功':results.get(item.movie.id)?.status==='failed'?'失败，待重试':'待删除' }}</span>
        <p class="duplicate-path"><span class="danger">删除：</span>{{ item.movie.library_name }} · {{ date(item.movie.created_at) }} · {{ item.movie.path_length }} 字符</p>
        <p class="mono duplicate-path" data-private-path>{{ item.movie.source_path }}</p>
        <p v-for="part in item.movie.additional_parts" :key="part" class="mono duplicate-path" data-private-path>分段：{{ part }}</p>
        <p class="duplicate-path">保留：{{ item.keep.library_name }} · {{ date(item.keep.created_at) }} · {{ item.keep.path_length }} 字符</p>
        <p class="mono duplicate-path" data-private-path>{{ item.keep.source_path }}</p>
        <template v-if="results.get(item.movie.id)?.status==='failed'">
          <p class="auth-error">失败原因：{{ results.get(item.movie.id)?.error }}</p>
          <p v-for="path in results.get(item.movie.id)?.removed || []" :key="path" class="mono duplicate-path" data-private-path>此副本远程已删除：{{ path }}</p>
        </template>
      </article>
    </div>
    <div v-if="listed.length>pageSize" class="duplicate-pagination"><button class="btn btn-sm" :disabled="!offset" @click="offset-=pageSize">上一页清单</button><span>{{ offset+1 }}–{{ Math.min(offset+pageSize,listed.length) }} / {{ listed.length }}</span><button class="btn btn-sm" :disabled="offset+pageSize>=listed.length" @click="offset+=pageSize">下一页清单</button></div>
    <template v-if="!running && (pending.length || failed.length)">
      <label class="check duplicate-delete-check"><input id="duplicate-bulk-confirmed" v-model="confirmed" type="checkbox">我已核对{{ pending.length?'待删':'失败' }}清单与保留副本，确认逐项核验并{{ pending.length?'批量删除':'仅重试失败项' }}</label>
      <button id="duplicate-bulk-confirm" class="btn danger" :disabled="!confirmed" @click="run">{{ !pending.length?`重新核验并重试失败 ${failed.length} 份`:results.size || stopRequested?`继续处理未完成 ${pending.length} 份`:'确认批量删除' }}</button>
    </template>
  </dialog>
</template>
