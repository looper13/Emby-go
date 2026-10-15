<script setup lang="ts">
import { onMounted, onScopeDispose, ref } from 'vue';
import {
  loadPlayerFactory,
  managePlayer,
  type PlayerOptions,
} from '../../lib/player';
import { useToastStore } from '../../stores/toasts';
import { apiClient } from '../../api/client';
const props = defineProps<{ options: PlayerOptions }>();
const emit = defineEmits<{ close: [] }>();
const shell = ref<HTMLElement>();
const stage = ref<HTMLElement>();
const closeButton = ref<HTMLButtonElement>();
const toasts = useToastStore();
let active = true;
let session: ReturnType<typeof managePlayer> | undefined;
let progressTimer: ReturnType<typeof setInterval> | undefined;
let began = false,
  resumed = false;
function reportProgress(stopped = false) {
  const video = session?.player.video;
  if (
    !began ||
    !props.options.itemId ||
    !video ||
    !Number.isFinite(video.currentTime) ||
    video.currentTime <= 0
  )
    return;
  void apiClient
    .request(`/Sessions/Playing/${stopped ? 'Stopped' : 'Progress'}`, {
      auth: 'required',
      method: 'POST',
      json: {
        ItemId: props.options.itemId,
        PositionTicks: Math.round(video.currentTime * 10000000),
      },
    })
    .catch(() => {});
}
function keyboard(event: KeyboardEvent) {
  if (document.getElementById('scrape-overlay')) return;
  if (event.key === 'Escape') {
    event.preventDefault();
    event.stopImmediatePropagation();
    emit('close');
  }
  if (event.key !== 'Tab' || !shell.value) return;
  const candidates = Array.from(
    shell.value.querySelectorAll<HTMLElement>(
      'button, a[href], input, select, [tabindex]:not([tabindex="-1"])',
    ),
  ).filter(
    (el) => !el.hasAttribute('disabled') && el.getClientRects().length > 0,
  );
  const first = candidates[0] ?? closeButton.value;
  const last = candidates.at(-1) ?? first;
  if (
    event.shiftKey &&
    (document.activeElement === first ||
      !shell.value.contains(document.activeElement))
  ) {
    event.preventDefault();
    last?.focus();
  } else if (
    !event.shiftKey &&
    (document.activeElement === last ||
      !shell.value.contains(document.activeElement))
  ) {
    event.preventDefault();
    first?.focus();
  }
}
onMounted(async () => {
  document.body.classList.add('player-open');
  closeButton.value?.focus();
  window.addEventListener('keydown', keyboard, true);
  try {
    const factory = await loadPlayerFactory(props.options.type);
    if (active && stage.value) {
      session = managePlayer(stage.value, props.options, factory, (message) =>
        toasts.show(message, 'error'),
      );
      if (props.options.itemId) {
        session.player.on('video:loadedmetadata', () => {
          if (!resumed && session?.player.video && props.options.resumeTicks) {
            session.player.video.currentTime =
              props.options.resumeTicks / 10000000;
            resumed = true;
          }
        });
        session.player.on('video:playing', () => {
          began = true;
        });
        session.player.on('video:ended', () => {
          void apiClient
            .request(
              `/Users/1/PlayedItems/${encodeURIComponent(props.options.itemId!)}`,
              { auth: 'required', method: 'POST' },
            )
            .catch(() => {});
        });
        progressTimer = setInterval(() => reportProgress(), 10000);
      }
    }
  } catch {
    if (active) {
      toasts.show('播放器加载失败，请刷新后重试', 'error');
      emit('close');
    }
  }
});
onScopeDispose(() => {
  active = false;
  clearInterval(progressTimer);
  reportProgress(true);
  session?.destroy();
  window.removeEventListener('keydown', keyboard, true);
  document.body.classList.remove('player-open');
});
</script>
<template>
  <div id="player-overlay" class="player-overlay" @click.self="emit('close')">
    <div
      ref="shell"
      class="player-shell"
      role="dialog"
      aria-modal="true"
      aria-labelledby="player-title"
    >
      <div class="player-head">
        <strong id="player-title">{{ options.title }}</strong
        ><button
          id="player-close"
          ref="closeButton"
          class="icon-btn"
          title="关闭 (Esc)"
          @click="emit('close')"
        >
          ✕
        </button>
      </div>
      <div id="player-stage" ref="stage" class="player-stage"></div>
    </div>
  </div>
</template>
