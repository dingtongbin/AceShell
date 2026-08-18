import { createApp } from 'vue'
import App from './App.vue'

document.addEventListener('contextmenu', (e) => {
  e.preventDefault()
})

createApp(App).mount('#app')
