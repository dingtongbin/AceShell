import Panel from './Panel.vue'
import Tool from './Tool.vue'
import styleText from './style.css?inline'

// 注入插件样式(一次性; 选择器统一前缀 .aceshell-plugin-ping 防全局污染)。
// data-aceshell-plugin 是宿主失效协议的标记: 插件卸载/重载时宿主按它移除样式。
if (!document.getElementById('aceshell-plugin-ping-style')) {
  const style = document.createElement('style')
  style.id = 'aceshell-plugin-ping-style'
  style.setAttribute('data-aceshell-plugin', 'ping')
  style.textContent = styleText
  document.head.appendChild(style)
}

// 宿主契约: components 键 = 协议 ViewInfo.componentId / TabSpec.componentId
// panel = 侧栏历史记录面板; tool = 工作台标签页(TabSpec componentId: "tool")
export default {
  components: {
    panel: Panel,
    tool: Tool,
  },
}
