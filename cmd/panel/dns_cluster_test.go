package main

import "testing"

// The checklist is the guidance, so it is tested as guidance: for each real
// situation, does it name the ONE thing that is actually wrong?
//
// The screen it replaced drew a green tick beside every nameserver name that
// resolved to this server. On a lone machine both names do — so it drew two
// green ticks over the exact failure the pair exists to prevent, and the
// operator said so plainly: "you are guiding me terribly."
//
// Kontrol listesi rehberliğin kendisidir; bu yüzden rehberlik olarak sınanır:
// her gerçek durum için, gerçekten yanlış olan TEK şeyi adıyla söylüyor mu?
//
// Yerine geçtiği ekran, bu sunucuya çözülen her ad sunucusu adının yanına
// yeşil bir tik çiziyordu. Tek başına bir makinede iki ad da öyle çözülür —
// yani çiftin engellemek için var olduğu arızanın üstüne iki yeşil tik
// çiziyordu ve operatör bunu açıkça söyledi: "berbat yönlendiriyorsun."
func TestPlanStepsNamesTheRealProblem(t *testing.T) {
	const here, peer = "72.62.38.15", "2.25.80.4"
	facts := func(ip1, ip2 string) []nameserverFact {
		f := []nameserverFact{{Host: "ns1.celikhost.com"}, {Host: "ns2.celikhost.com"}}
		for i, ip := range []string{ip1, ip2} {
			if ip != "" {
				f[i].IPs = []string{ip}
				f[i].PointsHere = ip == here
			}
		}
		return f
	}
	codes := func(steps []clusterStep) map[string]clusterStep {
		m := map[string]clusterStep{}
		for _, s := range steps {
			m[s.Code] = s
		}
		return m
	}

	// Standalone: not a fault, but the consequence must be stated, not hidden.
	// Tek başına: arıza değil, ama sonucu gizlenmeyip söylenmeli.
	if got := codes(planSteps("standalone", here, "", facts(here, here))); len(got) != 1 || got["aloneNoBackup"].Code == "" {
		t.Errorf("standalone must say it has no backup, got %v", got)
	}

	// The operator's exact situation: a pair chosen, but BOTH names still on
	// this machine. That is the headline, and it must come first.
	// Operatörün tam durumu: çift seçilmiş ama İKİ ad da bu makinede. Manşet
	// bu ve en başta gelmeli.
	steps := planSteps("primary", here, peer, facts(here, here))
	if steps[0].Code != "bothNamesHere" {
		t.Errorf("first step = %q, want bothNamesHere — it is the whole problem", steps[0].Code)
	}
	if got := codes(steps); got["otherNameAtPeer"].Done {
		t.Error("with both names here, the second name is NOT at the peer")
	}

	// Correctly split pair: one name here, one at the other server.
	// Doğru bölünmüş çift: bir ad burada, biri diğer sunucuda.
	got := codes(planSteps("primary", here, peer, facts(here, peer)))
	if !got["oneNameHere"].Done || !got["otherNameAtPeer"].Done {
		t.Errorf("a correctly split pair must tick both name steps, got %+v", got)
	}
	if _, bad := got["bothNamesHere"]; bad {
		t.Error("a correctly split pair must not be told both names are here")
	}
	// The step that happens on the OTHER machine can never be verified from
	// here, so it must be marked manual rather than silently ticked.
	// ÖBÜR makinede olan adım buradan asla doğrulanamaz; bu yüzden sessizce
	// işaretlenmek yerine elle-yapılır diye işaretlenmeli.
	if !got["peerIsSecondary"].Manual {
		t.Error("configuring the other server cannot be verified from here — it must be a manual step")
	}

	// A name that resolves to a third machine is not "at the peer", and the
	// checklist must still point at the peer's address as the target.
	// Üçüncü bir makineye çözülen ad "diğer sunucuda" değildir ve liste hedef
	// olarak yine eşin adresini göstermelidir.
	got = codes(planSteps("primary", here, peer, facts(here, "203.0.113.7")))
	if got["otherNameAtPeer"].Done {
		t.Error("a name pointing at a third machine must not count as pointing at the peer")
	}
	if len(got["otherNameAtPeer"].Args) == 0 || got["otherNameAtPeer"].Args[len(got["otherNameAtPeer"].Args)-1] != peer {
		t.Errorf("the step must name the peer address as the target, got %v", got["otherNameAtPeer"].Args)
	}

	// A secondary is told to set the OTHER server as primary, not as secondary.
	// İkincile, öbür sunucuyu ikincil değil BİRİNCİL yapması söylenir.
	got = codes(planSteps("secondary", peer, here, facts(peer, here)))
	if _, ok := got["peerIsPrimary"]; !ok {
		t.Error("a secondary must be told the other server is the primary")
	}
}
