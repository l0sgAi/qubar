<template>
  <div v-if="images.length || uploading" class="upload-image-wall">
    <NImageGroup>
      <div class="wall-grid" :style="{ gap: gap + 'px' }">
        <div v-for="(url, idx) in images" :key="'img-' + idx" class="wall-item">
          <NImage
            :src="url"
            :width="thumbSize"
            :height="thumbSize"
            object-fit="cover"
            lazy
            preview-src=""
            :style="{ borderRadius: '6px' }"
          />
          <button class="wall-remove" :title="t('common.delete')" @click="$emit('remove', idx)">
            <svg :width="removeIconSize" :height="removeIconSize" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
              <line x1="18" y1="6" x2="6" y2="18"></line>
              <line x1="6" y1="6" x2="18" y2="18"></line>
            </svg>
          </button>
        </div>
        <div
          v-for="n in uploadingCount"
          :key="'loading-' + n"
          class="wall-item wall-loading"
          :style="{ width: thumbSize + 'px', height: thumbSize + 'px' }"
        >
          <NSpin v-if="slotProgress(n) == null" :size="Math.round(thumbSize / 4)" />
          <span v-else class="wall-progress">{{ slotProgress(n) }}%</span>
        </div>
      </div>
    </NImageGroup>
    <span v-if="maxCount" class="wall-count">{{ images.length + uploadingCount }} / {{ maxCount }}</span>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { NImage, NImageGroup, NSpin } from 'naive-ui'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  images: { type: Array, default: () => [] },
  uploading: { type: Boolean, default: false },
  uploadingCount: { type: Number, default: 0 },
  maxCount: { type: Number, default: 0 },
  thumbSize: { type: Number, default: 80 },
  gap: { type: Number, default: 8 },
  // 每个上传槽位的真实进度（0-100），无进度时该槽显示 NSpin
  progress: { type: Array, default: () => [] }
})

defineEmits(['remove'])

const { t } = useI18n()
const removeIconSize = computed(() => (props.thumbSize >= 80 ? 14 : 12))
const slotProgress = (n) => props.progress[n - 1]
</script>

<style scoped>
.upload-image-wall {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.wall-grid {
  display: flex;
  flex-wrap: wrap;
}

.wall-item {
  position: relative;
  line-height: 0;
}

.wall-remove {
  position: absolute;
  top: -4px;
  right: -4px;
  width: 18px;
  height: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 77, 79, 0.9);
  border: none;
  border-radius: 50%;
  color: #fff;
  cursor: pointer;
  padding: 0;
  opacity: 0;
  transition: opacity 0.2s;
}

.wall-item:hover .wall-remove {
  opacity: 1;
}

.wall-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 255, 255, 0.06);
  border-radius: 6px;
}

.wall-progress {
  font-size: 13px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.85);
}

.wall-count {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.35);
  white-space: nowrap;
  margin-left: 12px;
}
</style>
