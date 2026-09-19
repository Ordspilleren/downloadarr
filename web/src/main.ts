import { mount } from 'svelte'
import App from './App.svelte'
import './app.css'

const target = document.getElementById('app')
if (!target) {
  throw new Error('failed to find the #app mount point')
}

export default mount(App, { target })
