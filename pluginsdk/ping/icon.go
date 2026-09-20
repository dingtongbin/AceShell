package main

import "encoding/base64"

// pingIcon 侧栏图标(SVG → dataURI)。
// 黑白线条风格: 中点 + 两侧声波弧线, 中性灰在深浅两套宿主主题下均可读。
// 注: 图标以 <img> 渲染, SVG 内无法继承宿主 currentColor, 故取固定中性灰。
var pingIcon = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none">` +
		`<circle cx="12" cy="12" r="1.7" fill="#9aa3ad"/>` +
		`<path d="M7.76 16.24 A6 6 0 0 1 7.76 7.76" stroke="#9aa3ad" stroke-width="1.7" stroke-linecap="round"/>` +
		`<path d="M16.24 7.76 A6 6 0 0 1 16.24 16.24" stroke="#9aa3ad" stroke-width="1.7" stroke-linecap="round"/>` +
		`<path d="M5.28 18.72 A9.5 9.5 0 0 1 5.28 5.28" stroke="#9aa3ad" stroke-width="1.7" stroke-linecap="round" opacity="0.55"/>` +
		`<path d="M18.72 5.28 A9.5 9.5 0 0 1 18.72 18.72" stroke="#9aa3ad" stroke-width="1.7" stroke-linecap="round" opacity="0.55"/>` +
		`</svg>`))
