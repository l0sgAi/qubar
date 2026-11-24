<template>
  <!--
    统一下拉菜单封装：全项目共用的暗色玻璃 + 整行铺满高亮样式在
    src/assets/main.css 的「全局下拉菜单主题样式」区块（菜单 render-to-body，
    只能写全局），本组件负责统一触发方式/弹出位置等交互默认值。
    其余 NDropdown props/事件（如 show、animated、menu-props）经 $attrs 透传。
  -->
  <NDropdown
    :options="options"
    :trigger="trigger"
    :placement="placement"
    :render-label="renderLabel"
    v-bind="$attrs"
    @select="handleSelect"
  >
    <slot />
  </NDropdown>
</template>

<script setup>
import { NDropdown } from 'naive-ui'

defineOptions({ inheritAttrs: false })

defineProps({
  options: { type: Array, required: true },
  trigger: { type: String, default: 'hover' },
  placement: { type: String, default: 'bottom-end' },
  renderLabel: { type: Function, default: undefined }
})

const emit = defineEmits(['select'])

const handleSelect = (key, option) => {
  emit('select', key, option)
}
</script>
