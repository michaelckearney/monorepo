import { createApp } from "vue";
import App from "./App.vue";
import { openLinksInBrowser } from "@desktop";
import "./style.css";

openLinksInBrowser();
createApp(App).mount("#app");
