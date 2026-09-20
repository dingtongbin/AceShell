import Panel from './Panel.vue'
import styleText from './style.css?inline'

// 注入插件样式(一次性; 选择器统一前缀 .aceshell-plugin-ping 防全局污染)
if (!document.getElementById('aceshell-plugin-ping-style')) {
  const style = document.createElement('style')
  style.id = 'aceshell-plugin-ping-style'
  style.textContent = styleText
  document.head.appendChild(style)
}

// 宿主契约: components 键 = 协议 ViewInfo.componentId / TabSpec.componentId
export default {
  components: {
    panel: Panel,
  },
}
