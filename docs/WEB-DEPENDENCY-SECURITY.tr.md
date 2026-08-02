# Web bağımlılığı güvenliği

## React Router

CelikPanel, Vite ile istemci tarafında çalışan bir uygulamadır.
`BrowserRouter` kullanır; React Server Components, framework/server kipi,
server action, `createRequestHandler`, sunucu tarafı render veya React Router
sunucu serileştirme yollarını etkinleştirmez.

`react-router-dom`, `7.18.2` sürümüne sabitlenmiştir. Bu sürüm eski sürümlerdeki
yönlendirme, istemci navigasyonu, manifest, SSR ve serileştirme açıklarını
kapatır. 2026-07-29 itibarıyla `npm audit --omit=dev`, React Router'ın `8.2.0`
sürümüne kadar GHSA-qwww-vcr4-c8h2 uyarısını göstermektedir. Uyarı yalnız
CelikPanel'de bulunmayan RSC/server-action istek işleme yoluyla ilgilidir.

Bu nedenle bulgu mevcut CelikPanel web mimarisinde erişilebilir değildir.
Aşağıdakilerden biri eklenmeden önce karar yeniden incelenmelidir:

- React Server Components veya React Router framework/server kipinin açılması;
- server action ya da React Router request handler eklenmesi;
- sunucu tarafı render veya önceden render edilmiş yönlendirme eklenmesi;
- serileştirilmiş React Router sunucu yüklerinin kabul edilmesi.

Raporu susturmak için `npm audit fix --force` kullanılmamalıdır. Bu karar
anında komut, istemci tarafında açık yönlendirme ve XSS uyarıları olan bir sürüm
önermektedir. Yükseltme ancak RSC uyarısını gideren ve erişilebilir istemci
bulgularını geri getirmeyen bir sürüm yayımlandığında yapılmalıdır.

## Doğrulama

Her web yayını için:

1. `npm ci --no-audit --no-fund` çalıştırın;
2. `npm run build` çalıştırın;
3. yalnızca açık işletmen onayından sonra `npm audit --omit=dev` çalıştırın;
   bu ağ denetimi paket adlarını ve sürümlerini yapılandırılmış npm kayıt
   servisine gönderir. Onay tam olarak şu cümleyi içermelidir: “Paket adları ve
   sürümlerinin npm’in açık denetim servisine gönderilmesini onaylıyorum.”;
4. yukarıdaki server/RSC API'lerinin uygulamada hâlâ bulunmadığını doğrulayın;
5. değişen uyarı aralığını yayın incelemesine kaydedin.
