<script setup lang="ts">
import { ref } from 'vue';
import { Plus } from '@lucide/vue';
defineProps<{ submitting: boolean }>();
const emit = defineEmits<{ submit: [payload: { Name: string; Path: string }] }>();
const name = ref(''); const path = ref('');
</script>
<template>
  <form id="library-form" class="field-grid" @submit.prevent="!submitting && emit('submit', { Name: name, Path: path })">
    <div class="field"><label for="lib-name">名称</label><input id="lib-name" v-model="name" name="Name" placeholder="如 AV / FC2" required></div>
    <div class="field"><label for="lib-path">目录路径</label><input id="lib-path" v-model="path" name="Path" placeholder="服务器上的绝对路径" required></div>
    <div class="form-foot" style="grid-column:1/-1;margin:2px 0 0"><button class="btn btn-accent" :disabled="submitting"><Plus :size="15" :stroke-width="1.8" aria-hidden="true" /><span>添加</span></button></div>
  </form>
</template>
