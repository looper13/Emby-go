<script setup lang="ts">
import { computed } from 'vue';
const props = defineProps<{ label: string; value?: string | number | string[] | null; entityKey?: string }>(); const emit = defineEmits<{ entity: [key: string, value: string] }>();
const values = computed(() => (Array.isArray(props.value) ? props.value : [props.value]).filter(Boolean).map(String));
</script>
<template><div v-if="values.length" class="detail-meta-row"><dt>{{ label }}</dt><dd><template v-for="(value, index) in values" :key="value"><span v-if="index">{{ ' ' }}</span><button v-if="entityKey" class="entity-link" :data-entity-key="entityKey" :data-entity-value="value" @click="emit('entity', entityKey, value)">{{ value }}</button><template v-else>{{ value }}</template></template></dd></div></template>
