package main

import "fmt"

// celikPanelSitePlaceholder is the single canonical placeholder contract used
// by both site provisioning and the one-click installer. Exact byte matching
// lets the installer replace only content CelikPanel itself generated.
func celikPanelSitePlaceholder(domain, projectType string) (string, []byte) {
	placeholderBody := `  <p>CelikPanel</p>`
	placeholderName := "index.html"
	if projectType == "php" {
		placeholderBody = `  <p>CelikPanel · PHP <?php echo htmlspecialchars(PHP_VERSION); ?> · <?php echo date('Y'); ?></p>`
		placeholderName = "index.php"
	}
	content := fmt.Sprintf(`<!doctype html>
<html lang="tr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
  body{font-family:system-ui,sans-serif;display:grid;place-items:center;min-height:100vh;margin:0;background:#0f172a;color:#e2e8f0}
  main{text-align:center;padding:2rem}
  h1{font-weight:600}
  p{color:#94a3b8}
</style>
</head>
<body>
<main>
  <h1>%s</h1>
  <p>Bu site hazırlanıyor. / This site is being prepared.</p>
%s
</main>
</body>
</html>
`, domain, domain, placeholderBody)
	return placeholderName, []byte(content)
}
