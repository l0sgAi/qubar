import { useRoute } from 'vue-router'
import { applyPageTitle, setDynamicTitleData } from '@/utils/pageTitle'

// 详情页数据加载成功后，用具体内容（帖子标题/圈子名/用户名）覆盖标签页标题。
// key 为 title 命名空间下的模板（如 'title.postDetailName'），params 为插值参数。
// 加载失败时不调用即可自动回退通用标题；语言切换时由 applyPageTitle 按当前语言重建
export const usePageTitle = () => {
  const route = useRoute()

  const setTitleData = (key, params) => {
    setDynamicTitleData(route.name, route.path, key, params)
    applyPageTitle(route)
  }

  return { setTitleData }
}
