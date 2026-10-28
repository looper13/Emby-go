<script setup lang="ts">
import { useCardSize } from '../../composables/useCardSize';

const { cardSize, setSize } = useCardSize();
</script>

<template>
  <div class="card-size-selector" role="group" aria-label="卡片尺寸">
    <button type="button" class="card-size-btn" :class="{ 'is-active': cardSize === 'compact' }" :aria-pressed="cardSize === 'compact'" title="紧凑卡片 (小图)" aria-label="紧凑尺寸" @click="setSize('compact')">小</button>
    <button type="button" class="card-size-btn" :class="{ 'is-active': cardSize === 'normal' }" :aria-pressed="cardSize === 'normal'" title="标准卡片 (默认)" aria-label="标准尺寸" @click="setSize('normal')">中</button>
    <button type="button" class="card-size-btn" :class="{ 'is-active': cardSize === 'large' }" :aria-pressed="cardSize === 'large'" title="舒适大图 (放大)" aria-label="大图尺寸" @click="setSize('large')">大</button>
  </div>
</template>
