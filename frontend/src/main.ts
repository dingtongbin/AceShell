import { createApp } from 'vue'
import App from './App.vue'
import i18n from './i18n'

document.addEventListener('contextmenu', (e) => {
  e.preventDefault()
})

createApp(App).use(i18n).mount('#app')