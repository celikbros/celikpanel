'use strict';
const tr=document.documentElement.lang==='tr';
const tokenField=document.querySelector('#email-token');
if(tokenField){const token=new URLSearchParams(location.hash.slice(1)).get('token');if(token&&/^[a-f0-9]{64}$/.test(token))tokenField.value=token;history.replaceState(null,'',location.pathname+location.search);}
document.querySelectorAll('[data-copy]').forEach(button=>button.addEventListener('click',async()=>{const node=document.getElementById(button.dataset.copy);try{await navigator.clipboard.writeText(node.textContent);button.textContent=tr?'Kopyalandı':'Copied';}catch{button.textContent=tr?'Metni seçip kopyalayın':'Select and copy the text';const selection=getSelection();selection.removeAllRanges();const range=document.createRange();range.selectNodeContents(node);selection.addRange(range);}}));
document.querySelectorAll('form').forEach(form=>form.addEventListener('submit',()=>{const button=form.querySelector('button[type=submit]');if(button){button.disabled=true;button.textContent=tr?'İşleniyor…':'Working…';}}));
addEventListener('pageshow',()=>document.querySelectorAll('button:disabled').forEach(button=>{button.disabled=false;}));
