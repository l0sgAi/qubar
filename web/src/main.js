import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import i18n from './locales'
import './assets/main.css'
import 'cropperjs/dist/cropper.css'

const app = createApp(App)

app.use(router)
app.use(i18n)
app.mount('#app')
