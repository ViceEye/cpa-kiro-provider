import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { viteSingleFile } from 'vite-plugin-singlefile';

// Pre-head inline script:
// 1. Hide <html> until data-cpa-theme is stamped, so the first paint is
//    never a wrong default color.
// 2. Set the iframe element's own background from the host CSS variable;
//    the iframe document cannot style that outer element through CSS.
// 3. Read the host panel's data-theme attribute and mirror it.
// 4. Re-show <html>.
// Pairs with the html:not([data-cpa-theme]){visibility:hidden} guard in
// src/style.css. Without that guard a white/default-color first frame
// would flash before this script can run.
const FLASH_FIX = `<script>/*flash-fix*/(function(){var d=document.documentElement;d.style.visibility='hidden';var p;try{if(window.parent!==window){var pr=window.parent.document.documentElement;p=pr.getAttribute('data-theme');var bg=window.parent.getComputedStyle(pr).getPropertyValue('--bg-secondary').trim()||window.parent.getComputedStyle(window.parent.document.body).backgroundColor;var f=window.frameElement;if(f&&bg){f.style.background=bg;for(var h=f.parentElement;h;h=h.parentElement){h.style.background=bg;if(h.tagName==='MAIN')break}}}}catch(e){}var m={dark:'dark',white:'white',light:'white'};d.setAttribute('data-cpa-theme',m[p]||'white');d.style.visibility=''})();<\/script>`;

const flashFixPlugin = {
  name: 'flash-fix',
  enforce: 'pre',
  transformIndexHtml(html) {
    return html.replace(/^/, FLASH_FIX);
  },
};

export default defineConfig({
  plugins: [
    flashFixPlugin,
    react(),
    viteSingleFile(),
  ],
  build: { cssCodeSplit: false, assetsInlineLimit: 100000000 },
});
