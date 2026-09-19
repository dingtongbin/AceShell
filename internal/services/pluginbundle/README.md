# 捆绑插件载荷

构建期由 `wails3 task plugins:build` 填充(每插件一个子目录), 随主程序嵌入发版。
除本文件外内容不入库。目录结构:

    pluginbundle/
      ping/
        ping.exe
        plugin.json
        dist/entry.js
        ...
