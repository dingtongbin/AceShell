import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { SHARED_DEPS, pluginLibDefaults, pluginBuildDefaults } from '../../vite-preset.mjs'

// 插件前端构建: 自包含 ESM(单文件 entry.js)。
// 关键 1: lib 模式不会自动注入 NODE_ENV define, vue 的 esm-bundler 产物含
// process.env.NODE_ENV 引用, 缺失会在浏览器抛 "process is not defined"。
// 关键 2: vue 必须 external(SHARED_DEPS 契约)——浏览器经宿主 import map 解析到
// 宿主唯一 Vue 实例; 若自打捆绑, 同页双实例会白屏/响应式失效。
export default defineConfig({
  plugins: [vue()],
  define: {
    'process.env.NODE_ENV': JSON.stringify('production'),
  },
  build: {
    lib: {
      entry: 'src/entry.ts',
      ...pluginLibDefaults,
    },
    outDir: '../dist',
    emptyOutDir: true,
    ...pluginBuildDefaults,
    rollupOptions: {
      external: SHARED_DEPS,
    },
  },
})
