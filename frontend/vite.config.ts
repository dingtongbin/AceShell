import { defineConfig, type Plugin } from "vite";
import vue from "@vitejs/plugin-vue";
import wails from "@wailsio/runtime/plugins/vite";
import { createRequire } from "node:module";
import path from "node:path";
import fs from "node:fs";

/**
 * 生成宿主 Vue shim(frontend/dist/plugins-host-vue.js)。
 * 导出面在构建时从本仓库实际安装的 vue 自动枚举 —— 版本永远与宿主同步, 无手工清单。
 * 构建产物随 //go:embed 进入二进制, main.go 读取后交给 PluginService 在
 * /plugins/_host/vue.js 同源伺服; 插件 ESM 经 index.html 的 import map("vue")
 * 解析到此文件, 复用宿主唯一的 Vue 实例。
 */
function hostVueShim(): Plugin {
  return {
    name: "aceshell-host-vue-shim",
    closeBundle() {
      const req = createRequire(path.resolve(process.cwd(), "package.json"));
      const vueModule = req("vue") as Record<string, unknown>;
      const names = Object.keys(vueModule).sort();
      const lines = [
        "/* 自动生成于宿主构建(hostVueShim): 导出面与宿主安装的 vue 完全一致, 勿手改。",
        " * 插件经 import map(\"vue\" → /plugins/_host/vue.js)复用宿主唯一 Vue 实例;",
        " * 插件产物自打 Vue 会造成同页双实例(白屏/响应式失效)。 */",
        "const V = globalThis.__ACESHELL_VUE__",
        "if (!V) throw new Error('[AceShell 插件] 宿主 Vue 未注册(__ACESHELL_VUE__), 宿主版本过旧, 请升级 AceShell')",
        "export default V",
      ];
      for (const n of names) {
        lines.push(`export const ${n} = V.${n}`);
      }
      const out = path.resolve(process.cwd(), "dist", "plugins-host-vue.js");
      fs.mkdirSync(path.dirname(out), { recursive: true });
      fs.writeFileSync(out, lines.join("\n") + "\n");
    },
  };
}

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
    // 插件前端资产: dev 下代理到宿主插件资产回环服务(pluginservice, 固定 8941);
    // 生产由 main.go 资产处理器进程内直连, 无需此代理。
    proxy: {
      "/plugins": "http://127.0.0.1:8941",
    },
  },
  plugins: [vue(), wails("./bindings"), hostVueShim()],
});
