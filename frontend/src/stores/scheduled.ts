import { ref } from 'vue'; import { defineStore } from 'pinia';
export const useScheduledStore = defineStore('scheduled', () => { const editingId = ref<number|null>(null); return {editingId}; });
