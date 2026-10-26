<script setup lang="ts">
defineProps<{ count?: number }>();
</script>

<template>
  <div v-for="n in (count ?? 12)" :key="n" class="skeleton-card" aria-hidden="true">
    <div class="skeleton-poster"></div>
    <div class="skeleton-line title"></div>
    <div class="skeleton-line meta"></div>
  </div>
</template>

<style scoped>
@keyframes shimmer {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(100%); }
}

.skeleton-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
  pointer-events: none;
  user-select: none;
}

.skeleton-poster {
  position: relative;
  width: 100%;
  aspect-ratio: 2 / 3;
  border-radius: 10px;
  background: var(--panel-2);
  border: 1px solid var(--line);
  overflow: hidden;
}

.skeleton-poster::after,
.skeleton-line::after {
  content: '';
  position: absolute;
  inset: 0;
  transform: translateX(-100%);
  background: linear-gradient(
    90deg,
    transparent 0%,
    rgba(255, 255, 255, 0.08) 50%,
    transparent 100%
  );
  animation: shimmer 1.6s infinite;
}

[data-theme="light"] .skeleton-poster::after,
[data-theme="light"] .skeleton-line::after {
  background: linear-gradient(
    90deg,
    transparent 0%,
    rgba(0, 0, 0, 0.06) 50%,
    transparent 100%
  );
}

.skeleton-line {
  position: relative;
  height: 14px;
  border-radius: 4px;
  background: var(--panel-2);
  overflow: hidden;
}

.skeleton-line.title {
  width: 80%;
}

.skeleton-line.meta {
  width: 50%;
  height: 11px;
}
</style>
