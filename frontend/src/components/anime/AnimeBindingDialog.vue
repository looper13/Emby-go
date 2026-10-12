<script setup lang="ts">
import { onMounted, onScopeDispose, ref } from 'vue';
import { animeApi, type AnimeBinding } from '../../api/anime';
import { useAdminRead } from '../../composables/useAdminRead';
import ReadState from '../common/ReadState.vue';
const props = defineProps<{
  target: string;
  title: string;
  binding: AnimeBinding;
}>();
const emit = defineEmits<{ close: []; saved: [] }>();
const dialog = ref<HTMLDialogElement>(),
  search = ref(''),
  selected = ref(props.binding.movie_id),
  busy = ref(false),
  saveError = ref(''),
  offset = ref(0);
const { data, error, loading, load } = useAdminRead((signal) =>
  animeApi.media(search.value, offset.value, signal),
);
onMounted(() => dialog.value?.showModal());
onScopeDispose(() => dialog.value?.close());
async function save(id: number) {
  if (busy.value) return;
  busy.value = true;
  saveError.value = '';
  try {
    await animeApi.bind(props.target, id, props.binding.revision);
    emit('saved');
    emit('close');
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : '关联失败';
  } finally {
    busy.value = false;
  }
}
function page(delta: number) {
  offset.value = Math.max(0, offset.value + delta);
  void load();
}
</script>
<template>
  <dialog
    ref="dialog"
    class="anime-dialog"
    aria-labelledby="anime-bind-title"
    @cancel.prevent="!busy && emit('close')"
  >
    <div class="panel-head">
      <h2 id="anime-bind-title">关联已有媒体 · {{ title }}</h2>
      <button class="btn" :disabled="busy" @click="emit('close')">关闭</button>
    </div>
    <p>请选择已入库的单文件 STRM。替换关联后，每份原媒体保留各自的观看记录。</p>
    <p v-if="binding.movie_id">当前媒体 ID：{{ binding.movie_id }}</p>
    <form
      class="anime-filters"
      @submit.prevent="
        offset = 0;
        load();
      "
    >
      <label class="anime-search"
        >媒体标题<input v-model="search" placeholder="搜索已入库媒体" /></label
      ><button class="btn">搜索</button>
    </form>
    <ReadState
      :error="error"
      :loading="loading"
      :empty="!data"
      @retry="load"
    /><label v-for="item in data?.items" :key="item.id" class="anime-candidate"
      ><input
        v-model="selected"
        type="radio"
        name="anime-media"
        :value="item.id"
        :disabled="
          !item.eligible || (item.occupied && item.id !== binding.movie_id)
        "
      />{{ item.name }}<small>{{ item.library_name }} · {{ item.path }}</small
      ><small v-if="!item.eligible">{{ item.reason }}</small
      ><small v-if="item.occupied">{{
        item.id === binding.movie_id ? '当前关联' : '已被其他章节占用'
      }}</small></label
    >
    <div class="anime-pagination">
      <button class="btn" :disabled="offset === 0" @click="page(-50)">
        上一页</button
      ><button
        class="btn"
        :disabled="offset + 50 >= (data?.total ?? 0)"
        @click="page(50)"
      >
        下一页
      </button>
    </div>
    <p v-if="saveError" role="alert">{{ saveError }}</p>
    <div class="row-actions">
      <button
        v-if="binding.movie_id"
        class="btn"
        :disabled="busy"
        @click="save(0)"
      >
        解除关联</button
      ><button
        class="btn btn-accent"
        :disabled="busy || !selected"
        @click="save(selected)"
      >
        {{ busy ? '保存中…' : '确认关联' }}
      </button>
    </div>
  </dialog>
</template>
