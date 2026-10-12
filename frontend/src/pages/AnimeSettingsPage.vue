<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { onBeforeRouteLeave } from 'vue-router';
import { animeApi, type AnimeSettings } from '../api/anime';
import { useAdminRead } from '../composables/useAdminRead';
import { useToastStore } from '../stores/toasts';
import ReadState from '../components/common/ReadState.vue';
const { data, error, loading, load, active } = useAdminRead(animeApi.settings),
  draft = ref<AnimeSettings | null>(null),
  busy = ref(false),
  message = ref(''),
  toasts = useToastStore();
const dirty = computed(
  () => JSON.stringify(draft.value) !== JSON.stringify(data.value),
);
watch(data, (value) => {
  if (value) draft.value = structuredClone(value);
});
onBeforeRouteLeave(
  () => !dirty.value || window.confirm('动漫设置尚未保存，确定离开？'),
);
function restore() {
  if (data.value) draft.value = structuredClone(data.value);
  message.value = '';
}
async function save() {
  if (!draft.value || busy.value) return;
  busy.value = true;
  message.value = '';
  try {
    const saved = await animeApi.saveSettings(draft.value);
    if (active()) {
      data.value = saved;
      toasts.show('动漫设置已保存');
    }
  } catch (e) {
    message.value = e instanceof Error ? e.message : '保存失败';
  } finally {
    busy.value = false;
  }
}
async function test() {
  if (!draft.value || busy.value) return;
  busy.value = true;
  message.value = '';
  try {
    await animeApi.test(draft.value.source.mikan_url);
    if (active()) message.value = '蜜柑与 Bangumi 连接成功，草稿未保存。';
  } catch (e) {
    if (active()) message.value = e instanceof Error ? e.message : '测试失败';
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <section class="panel">
    <div class="panel-head">
      <h2>动漫库设置 <small v-if="dirty">· 未保存</small></h2>
      <RouterLink class="btn" to="/anime">返回动漫库</RouterLink>
    </div>
    <ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
    <form v-if="draft" class="field-grid" @submit.prevent="save">
      <div class="field">
        <label for="anime-name">库名称</label
        ><input
          id="anime-name"
          v-model="draft.library.Name"
          required
          maxlength="100"
        />
      </div>
      <div class="field">
        <label for="anime-enabled">启用状态</label
        ><label class="check"
          ><input
            id="anime-enabled"
            v-model="draft.library.Enabled"
            type="checkbox"
          />启用动漫库</label
        >
      </div>
      <div class="field full">
        <label for="anime-mikan">蜜柑基础地址</label
        ><input
          id="anime-mikan"
          v-model="draft.source.mikan_url"
          type="url"
          required
          placeholder="https://mikan.tangbai.cc"
        />
        <p class="hint">
          此默认地址是当前配置的公共镜像。Bangumi 使用公开
          API，无需账号。关闭库后保留资料和人工关联。
        </p>
      </div>
      <div class="form-foot" style="grid-column: 1/-1">
        <button class="btn" type="button" :disabled="busy" @click="test">
          测试草稿连接</button
        ><button
          class="btn"
          type="button"
          :disabled="busy || !dirty"
          @click="restore"
        >
          恢复已保存值</button
        ><button class="btn btn-accent" :disabled="busy || !dirty">
          保存设置
        </button>
      </div>
      <p v-if="message" role="status" style="grid-column: 1/-1">
        {{ message }}
      </p>
    </form>
  </section>
  <section class="panel">
    <h2>资料同步</h2>
    <p>
      首次成功加载季度目录后，创建每 6
      小时运行一次的资料同步任务。时间与启用状态在计划任务页管理。
    </p>
    <RouterLink class="btn" to="/scheduled">管理计划任务</RouterLink
    ><RouterLink class="btn" to="/tasks">查看执行任务</RouterLink>
  </section>
</template>
