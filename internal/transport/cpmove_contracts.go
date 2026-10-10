package transport

// CpmoveMailAccount is one mailbox the archive's shadow files name.
//
// The password hash is a secret of the mailbox's owner and never leaves the
// server (11 Oct 2026; measured: the import preview answered it to the
// browser). So:
//
//   - HasPassword is all a preview says: the archive holds a password hash for
//     this mailbox, and an import keeps that password;
//   - CryptHash is filled by the Agent only when the Panel asks for it to
//     apply an import (CpmoveInspectRequest.IncludeMailHashes). It travels
//     between the two server processes over their local RPC, which encodes by
//     field name, and it is never encoded as JSON: no answer to a browser, no
//     stored answer and no log line built from this type can carry it.
//
// CpmoveMailAccount, arşivin shadow dosyalarının adını verdiği bir posta
// kutusudur. Parola özeti sunucudan asla çıkmaz: önizleme yalnızca HasPassword
// der; CryptHash yalnızca içe aktarım uygulanırken Agent'tan Panel'e yerel RPC
// üzerinden gider ve hiçbir zaman JSON olarak kodlanmaz.
type CpmoveMailAccount struct {
	Domain      string `json:"domain"`
	User        string `json:"user"`
	QuotaMB     int    `json:"quota_mb"`
	HasPassword bool   `json:"has_password"`
	CryptHash   string `json:"-"`
}

type CpmoveForwarder struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type CpmoveDNSRecord struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Prio    int    `json:"prio"`
}

type CpmoveDatabase struct {
	Name      string `json:"name"`
	DumpBytes int64  `json:"dump_bytes"`
}

type CpmoveInspectRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	Path                string `json:"path"`
	// IncludeMailHashes asks for each mailbox's password hash. Only the apply
	// of an import sets it; a preview never does.
	// Yalnızca içe aktarımın uygulanması ister; önizleme asla istemez.
	IncludeMailHashes bool `json:"-"`
}

type CpmoveInspectResponse struct {
	Username     string                       `json:"username"`
	MainDomain   string                       `json:"main_domain"`
	Domains      []string                     `json:"domains"`
	PublicHTML   bool                         `json:"public_html"`
	SiteBytes    int64                        `json:"site_bytes"`
	MailAccounts []CpmoveMailAccount          `json:"mail_accounts"`
	Forwarders   []CpmoveForwarder            `json:"forwarders"`
	DNSZones     map[string][]CpmoveDNSRecord `json:"dns_zones"`
	Databases    []CpmoveDatabase             `json:"databases"`
	Error        string                       `json:"error,omitempty"`
}

type CpmoveExtractRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	Path                string `json:"path"`
	SubscriptionID      int    `json:"subscription_id"`
	DomainID            int    `json:"domain_id"`
}

type CpmoveExtractResponse struct {
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
	Complete bool   `json:"complete"`
	Error    string `json:"error,omitempty"`
	// What the files step left out while it imported the rest (12 Oct 2026).
	// It used to leave these out without a word. Additive.
	//
	// Refused: members whose name is not one the step will place anywhere (an
	// absolute path). RefusedCount is how many there were; Refused holds at
	// most CpmoveRefusedMemberLimit of them, in archive order.
	//
	// Outside: members that are not below homedir/public_html, the one folder
	// this step copies. OutsideCount counts them (directories are not
	// counted); OutsideGroups says where they are, by the folder they are in,
	// largest first, at most CpmoveOutsideGroupLimit groups.
	//
	// Dosya adımının, geri kalanını içe aktarırken dışarıda bıraktıkları.
	// Eskiden tek söz etmeden bırakılıyorlardı. Eklemelidir.
	Refused       []CpmoveRefusedMember `json:"refused,omitempty"`
	RefusedCount  int                   `json:"refused_count,omitempty"`
	OutsideCount  int                   `json:"outside_count,omitempty"`
	OutsideGroups []CpmoveMemberGroup   `json:"outside_groups,omitempty"`
}

const (
	CpmoveRefusedMemberLimit = 20
	CpmoveOutsideGroupLimit  = 12
	// CpmoveRefusedAbsolutePath: the archive names the member with an
	// absolute path. Nothing was written for it, anywhere.
	CpmoveRefusedAbsolutePath = "absolute_path"
)

// CpmoveRefusedMember is one member the files step refused by its name. Name
// is bounded and holds no control characters.
type CpmoveRefusedMember struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// CpmoveMemberGroup counts the members outside the site folder that are in
// one folder of the archive ("homedir/mail", "mysql").
type CpmoveMemberGroup struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type CpmoveImportDBRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	Path                string `json:"path"`
	DumpName            string `json:"dump_name"`
	TargetDB            string `json:"target_db"`
}

type CpmoveImportDBResponse struct {
	Imported bool   `json:"imported"`
	Error    string `json:"error,omitempty"`
}
