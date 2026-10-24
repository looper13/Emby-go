<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, shallowRef } from 'vue';
import { ApiError } from '../../api/client';
import { duplicatesApi, duplicateRuleLabels, type DuplicateSelection, type DuplicateDeletePreview } from '../../api/duplicates';
import { usePageRequest } from '../../composables/usePageRequest';

const props=defineProps<{selection:DuplicateSelection; libraryName:string}>();
const emit=defineEmits<{close:[completed:number]}>();
const shell=ref<HTMLDialogElement>(),closeButton=ref<HTMLButtonElement>();
const scope=usePageRequest(),running=ref(false),confirmed=ref(false),next=ref(0),offset=ref(0),error=ref(''),phase=ref<'idle'|'preview'|'delete'>('idle');
const stopRequested=ref(false),preview=shallowRef<DuplicateDeletePreview|null>(null),removed=ref<string[]>([]),pageSize=20;
const current=computed(()=>props.selection.items[next.value]);
const page=computed(()=>props.selection.items.slice(offset.value,offset.value+pageSize));
let active=true;
function date(value:string) {return value && !Number.isNaN(Date.parse(value))?new Date(value).toLocaleString():'未知';}
function close() {if(!running.value) emit('close',next.value);}
function stop() {stopRequested.value=true;if(phase.value==='preview') scope.cancel();}
async function run() {
  if(running.value || !confirmed.value || !current.value) return;
  running.value=true;confirmed.value=false;error.value='';removed.value=[];stopRequested.value=false;
  while(active && !stopRequested.value && current.value) {
    const item=current.value,proof={keep_id:item.keep.id,selection_token:item.selection_token};
    try {
      phase.value='preview';preview.value=null;
      const request=scope.begin();
      const checked=await duplicatesApi.previewDelete(item.movie.id,props.selection.library_id,item.key,request.signal,proof);
      if(!active || stopRequested.value || !request.isCurrent()) break;
      preview.value=checked;phase.value='delete';
      // Fresh per-copy previews avoid expiring tokens while a long queue runs.
      await duplicatesApi.deleteCopy(item.movie.id,props.selection.library_id,item.key,checked,proof);
      if(!active) break;
      next.value++;preview.value=null;
    } catch(cause) {
      if(active && (!stopRequested.value || phase.value==='delete')) {
        error.value=(cause instanceof Error || cause instanceof DOMException) && cause.name==='TimeoutError'?'当前副本核验超时，请检查 STRM 与 AList 后继续。':cause instanceof Error || cause instanceof DOMException?cause.message:'批量删除失败';
        if(cause instanceof ApiError && cause.payload && typeof cause.payload==='object' && 'removed_remote_paths' in cause.payload && Array.isArray(cause.payload.removed_remote_paths)) removed.value=cause.payload.removed_remote_paths.filter((value):value is string=>typeof value==='string');
      }
      break;
    }
  }
  if(active) {running.value=false;phase.value='idle';confirmed.value=false;}
}
onMounted(()=>{if(shell.value?.showModal) shell.value.showModal();else shell.value?.setAttribute('open','');closeButton.value?.focus();});
onScopeDispose(()=>{active=false;scope.cancel();shell.value?.close?.();});
</script>

<template>
  <dialog ref="shell" class="duplicate-delete-dialog duplicate-bulk-dialog" aria-labelledby="duplicate-bulk-title" @cancel.prevent="close">
    <div class="duplicate-delete-head"><h3 id="duplicate-bulk-title">批量删除多余副本</h3><button ref="closeButton" class="btn btn-sm" :disabled="running" @click="close">{{ next===selection.total?'关闭':'取消 / 关闭' }}</button></div>
    <p><strong>{{ libraryName }}</strong> · {{ duplicateRuleLabels[selection.rule] }} · {{ selection.groups }} 组 · 全量选择 {{ selection.total }} 份</p>
    <p class="hint">待删清单覆盖全部结果页，只删除本次选中的副本。每组指定保留一份，执行时再次检查保留副本的索引和路径未变；不会读取保留副本的 STRM。加入时间是首次入库时间，路径长度按主文件完整路径的字符数计算；相同值保留 ID 较小的一份，未知时间排在有效时间后。</p>
    <p class="auth-error">将通过 AList 删除远程原文件，再清理本地 STRM（含 CD 分段）和影片索引，保留 NFO、封面及其他文件。请核对下方待删/保留路径，确认保留副本可播放。网盘删除不可跨系统回滚。</p>
    <p role="status">已完成 {{ next }} / {{ selection.total }} 份{{ running?`，正在${phase==='preview'?'核验':'删除'}第 ${next+1} 份`:next===selection.total?'，批量删除完成':next?'，队列已停止，可确认后继续':'' }}</p>
    <template v-if="running && current">
      <p class="mono duplicate-path" data-private-path>{{ current.movie.source_path }}</p>
      <p v-for="file in preview?.files || []" :key="file.local_path" class="mono duplicate-path" data-private-path>远程：{{ file.remote_path }}</p>
      <button class="btn btn-sm" :disabled="stopRequested" @click="stop">{{ stopRequested?'正在停止，当前删除完成后停下':'停止队列' }}</button>
      <p class="hint">每次只处理一个副本。停止或离开页面不会继续下一份；已发出的删除会完成当前副本的清理记账。</p>
    </template>
    <p v-if="error" class="auth-error" role="alert">{{ error }} 已完成的副本不会再次处理，失败项及后续项仍保留。网络响应丢失时请先重新查重确认当前状态；保留副本或路径变化需关闭后重新全量选择。</p>
    <p v-for="path in removed" :key="path" class="mono duplicate-path" data-private-path>当前副本远程已删除：{{ path }}</p>
    <div class="duplicate-bulk-list" tabindex="0" aria-label="全量待删及保留副本清单">
      <article v-for="(item,index) in page" :key="item.movie.id" class="duplicate-bulk-item">
        <strong class="mono">{{ item.key }}</strong><span class="hint"> · {{ offset+index<next?'已完成':'待删除' }}</span>
        <p class="duplicate-path"><span class="danger">删除：</span>{{ item.movie.library_name }} · {{ date(item.movie.created_at) }} · {{ item.movie.path_length }} 字符</p>
        <p class="mono duplicate-path" data-private-path>{{ item.movie.source_path }}</p>
        <p v-for="part in item.movie.additional_parts" :key="part" class="mono duplicate-path" data-private-path>分段：{{ part }}</p>
        <p class="duplicate-path">保留：{{ item.keep.library_name }} · {{ date(item.keep.created_at) }} · {{ item.keep.path_length }} 字符</p>
        <p class="mono duplicate-path" data-private-path>{{ item.keep.source_path }}</p>
      </article>
    </div>
    <div v-if="selection.total>pageSize" class="duplicate-pagination"><button class="btn btn-sm" :disabled="!offset" @click="offset-=pageSize">上一页清单</button><span>{{ offset+1 }}–{{ Math.min(offset+pageSize,selection.total) }} / {{ selection.total }}</span><button class="btn btn-sm" :disabled="offset+pageSize>=selection.total" @click="offset+=pageSize">下一页清单</button></div>
    <template v-if="!running && next<selection.total">
      <label class="check duplicate-delete-check"><input id="duplicate-bulk-confirmed" v-model="confirmed" type="checkbox">我已核对全量待删清单与保留副本，确认逐项核验并批量删除</label>
      <button id="duplicate-bulk-confirm" class="btn danger" :disabled="!confirmed" @click="run">{{ next || error?'重新核验并继续剩余副本':'确认批量删除' }}</button>
    </template>
  </dialog>
</template>
