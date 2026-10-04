CelikPanel teknik devrini alıyorsun.

Çalışma dizini: C:\CELIKBROS PROJECTS\celikpanel
Dal: feat/dns-artifact-separation
Kaynak devir commit'i: 3197ff84; ardından gelen kanıt/devir belgesi commit'ini de git log ile doğrula.

Önce AGENTS.md, docs/HANDOFF-2026-09-28.tr.md, ROADMAP.tr.md ve docs/RESILIENCE-CONTRACT.tr.md dosyalarını oku. Devir öncesi envanteri bugünkü kirli çalışma ağacı sanma. Mevcut kaynak ve kanıtları koru; git clean/reset veya toplu silme yapma.

Hedef: mevcut 1–4 sırasını tamamlayıp CelikPanel'i kullanıcının deneyebileceği kesin adaya ulaştırmak. Yeni genel mimari plan başlatma. Önce devirdeki tamamlanan/açık iddialarını kod ve kanıtlarla karşılaştır; ilk somut eksik üzerinde çalışmaya başla.

BIND birincil–PowerDNS ikincil ve PowerDNS birincil–BIND ikincil normal ürün akışında desteklenmeli. PowerDNS çiftli birincil kapısı şu an kapalı; deneylerin geçmesini teslim edilmiş özellik sayma ve kapıyı kanıtsız kaldırma. Boş sunucu kurulumu ile mevcut BIND'den PowerDNS'e motor geçişini ayrı değerlendir.

Mevcut sıra: 1) DNS kurtarma, 2) iki yönlü çift/topoloji kabulü, 3) güncelleme/geri alma/erişim ve hizmet bağımsızlığı kabulü, 4) kesin adayın incelemesi ve kullanıcı testi. AI asistanı bunlardan sonra gelir. Gereksiz kapsam genişletme ve tekrar testlerden kaçın; her sıranın bitişinde yol haritasını kanıtla güncelle. Yalnız gerçek engel veya gerekli kullanıcı kararı varsa dur.

Planlamayı Astra yapacak. Yararlı bağımsız zor işleri Sol'a, basit kontrol/belge işlerini Luna'ya devret; model yoksa açıkça söyle. Sırf kuralı yerine getirmek için ajan üretme.

Kurulu Frankfurt/Boston veya diğer panelleri kullanıcı adına hiçbir yöntemle güncelleme. Güncellemeyi yalnız kullanıcı panel arayüzünden başlatır. Canlı durumu doğrulamadan sağlıklı/güncel sayma; kullanıcıya açık kurtarma yolunu izle ve gizli canlı düzeltme yapma.
