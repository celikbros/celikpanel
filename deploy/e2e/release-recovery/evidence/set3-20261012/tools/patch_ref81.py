# set3: the published v0.1.0-alpha.81 as an unpatched baseline (owner_update_trial.py, build-upd1-artifacts.sh).
import sys
ROOT = r'C:\CELIKBROS PROJECTS\celikpanel\deploy\e2e\release-recovery' + '\\'


class Patch:
    def __init__(self, name):
        self.path = ROOT + name
        self.s = open(self.path, encoding='utf-8', newline='').read()
        assert '\r\n' not in self.s, name

    def rep(self, a, b, n=1):
        assert self.s.count(a) == n, (a[:80], self.s.count(a))
        self.s = self.s.replace(a, b)

    def save(self):
        open(self.path, 'w', encoding='utf-8', newline='').write(self.s)


p = Patch('owner_update_trial.py')
p.rep('''ALPHA80_COMMIT = "bd14d97efc5cfd19acd70ddf0edb9c6343317e2b"
''', '''ALPHA80_COMMIT = "bd14d97efc5cfd19acd70ddf0edb9c6343317e2b"
# set3: the published v0.1.0-alpha.81 tag. It already carries the D-027 acceptance-license seam and its guard, so a
# baseline built from it is the tag's own commit, unchanged (no fixture commit, no patched file).
ALPHA81_COMMIT = "a0beb7263d1f4ca72258f6b306f9111ba4e2a334"
''')
p.rep('''                        "baseline_policy": {"version": "v0.1.0-alpha.80", "current": 80, "previous": 79,
                                            "previous_version": "v0.1.0-alpha.79"}},
}''', '''                        "baseline_policy": {"version": "v0.1.0-alpha.80", "current": 80, "previous": 79,
                                            "previous_version": "v0.1.0-alpha.79"}},
    # set3: the baseline IS the tag commit (unpatched: the tag carries the seam); the candidates are the source
    # labelled as the release after it.
    "v0.1.0-alpha.81": {"commit": ALPHA81_COMMIT, "baseline": ("v0.1.0-alpha.81", 81),
                        "candidate": ("v0.1.0-alpha.82", 82), "unpatched": True,
                        "baseline_policy": {"version": "v0.1.0-alpha.81", "current": 81, "previous": 80,
                                            "previous_version": "v0.1.0-alpha.80"}},
}


def baseline_ref_patched(ref: str) -> tuple:
    """The files the baseline fixture changes over the published tag: the seam files, or none (set3)."""
    return () if BASELINE_REFS[ref].get("unpatched") else BASELINE_REF_PATCHED''')
p.rep('''    if LABEL_REF is not None:
        # upd7: the baseline is the published tag's tree with only the acceptance-license seam added.
        base["baseline"] = (''', '''    if LABEL_REF is not None and BASELINE_REFS[LABEL_REF].get("unpatched"):
        # set3: the baseline is the published tag's own commit; nothing is patched.
        base["baseline"] = (f"published-tag-{LABEL_REF}-commit-unchanged (the tag carries the D-027 acceptance-license "
                            "seam; bin/panel is built from the tag's source with the acceptance_license build tag); its "
                            "release policy, installer, update/rollback/recovery/bootstrap scripts, get.sh, web and Agent "
                            "are the tag's; installed by the real installer; fixture trust root enrolled; "
                            "not-production-release-admission; not the signed release archive")
        base["candidate"] = (f"unpublished-local-fixture-commit-over-the-source-labelled-{CANDIDATE_VERSION}; signed "
                             "with the disposable fixture key; served by the guest-loopback celikpanel.net fixture "
                             "origin; not a release")
    elif LABEL_REF is not None:
        # upd7: the baseline is the published tag's tree with only the acceptance-license seam added.
        base["baseline"] = (''')
p.rep('''        if (ref.get("tag_commit") != profile["commit"] or document["baseline"].get("parent") != profile["commit"]
                or ref.get("patched_files") != list(BASELINE_REF_PATCHED)
                or (BASELINE_VERSION, BASELINE_SEQUENCE) != profile["baseline"]):''', '''        if profile.get("unpatched"):
            # set3: the baseline is the tag's own commit; no file is patched.
            if (ref.get("tag_commit") != profile["commit"] or document["baseline"]["commit"] != profile["commit"]
                    or ref.get("patched_files") != [] or (BASELINE_VERSION, BASELINE_SEQUENCE) != profile["baseline"]):
                raise ValueError("the published-baseline artifact is not the tag's own commit "
                                 "(or configure_labels was not applied)")
        elif (ref.get("tag_commit") != profile["commit"] or document["baseline"].get("parent") != profile["commit"]
                or ref.get("patched_files") != list(BASELINE_REF_PATCHED)
                or (BASELINE_VERSION, BASELINE_SEQUENCE) != profile["baseline"]):''')
p.rep('''    profile = baseline_ref_profile(ref)
    head = subprocess.run(["git", "-C", str(repo), "rev-parse", "HEAD"], capture_output=True, text=True,
                          check=True).stdout.strip()''', '''    profile = baseline_ref_profile(ref)
    if profile.get("unpatched"):
        raise ValueError(f"{ref} carries the acceptance-license seam; its baseline is the tag commit itself")
    head = subprocess.run(["git", "-C", str(repo), "rev-parse", "HEAD"], capture_output=True, text=True,
                          check=True).stdout.strip()''')
p.save()

b = Patch('build-upd1-artifacts.sh')
b.rep('''    [[ $BASELINE_REF == v0.1.0-alpha.80 ]] || { echo "only --baseline-ref v0.1.0-alpha.80 is supported" >&2; exit 2; }''',
      '''    [[ $BASELINE_REF == v0.1.0-alpha.80 || $BASELINE_REF == v0.1.0-alpha.81 ]] \\
        || { echo "only --baseline-ref v0.1.0-alpha.80 or v0.1.0-alpha.81 is supported" >&2; exit 2; }''')
b.rep('''    git -C "$clone" checkout --quiet --detach "$tag_commit"
    python3 "$DRIVER" fixture-source --repo "$clone" --kind baseline-ref --baseline-ref "$BASELINE_REF" \\
        --source-commit "$source_commit" >&2
    git -C "$clone" add -A -- internal/licensing cmd/panel/license.go
    git -C "$clone" commit --quiet -m "test(fixture): upd7 baseline = published $BASELINE_REF + the D-027 acceptance-license seam only (disposable)"
    baseline=$(git -C "$clone" rev-parse HEAD)
    git -C "$clone" update-ref refs/upd1/baseline "$baseline"
    expected=$(python3 -c 'import importlib.util,sys;s=importlib.util.spec_from_file_location("o",sys.argv[1]);m=importlib.util.module_from_spec(s);sys.modules["o"]=m;s.loader.exec_module(m);print("\\n".join(m.BASELINE_REF_PATCHED))' "$DRIVER")''',
      '''    git -C "$clone" checkout --quiet --detach "$tag_commit"
    if [[ $BASELINE_REF == v0.1.0-alpha.81 ]]; then
        # set3: the published v0.1.0-alpha.81 already carries the acceptance-license seam and its guard, so the
        # baseline is the tag's own commit: no fixture commit, no patched file. A tag without them is refused.
        for needed in internal/licensing/acceptance_fixture.go internal/licensing/acceptance_off.go \\
                deploy/release-acceptance-license-guard.sh; do
            git -C "$clone" cat-file -e "$tag_commit:$needed" \\
                || { echo "$BASELINE_REF lacks $needed; it cannot be built unpatched" >&2; exit 1; }
        done
        baseline=$tag_commit
    else
    python3 "$DRIVER" fixture-source --repo "$clone" --kind baseline-ref --baseline-ref "$BASELINE_REF" \\
        --source-commit "$source_commit" >&2
    git -C "$clone" add -A -- internal/licensing cmd/panel/license.go
    git -C "$clone" commit --quiet -m "test(fixture): upd7 baseline = published $BASELINE_REF + the D-027 acceptance-license seam only (disposable)"
    baseline=$(git -C "$clone" rev-parse HEAD)
    fi
    git -C "$clone" update-ref refs/upd1/baseline "$baseline"
    expected=$(python3 -c 'import importlib.util,sys;s=importlib.util.spec_from_file_location("o",sys.argv[1]);m=importlib.util.module_from_spec(s);sys.modules["o"]=m;s.loader.exec_module(m);print("\\n".join(m.baseline_ref_patched(sys.argv[2])))' "$DRIVER" "$BASELINE_REF")''')
b.rep('''    good=$(commit_fixture good "test(fixture): upd7 good candidate labelled v0.1.0-alpha.81 after the published $BASELINE_REF (unpublished, disposable)" "$baseline")''',
      '''    good=$(commit_fixture good "test(fixture): upd7 good candidate labelled as the release after the published $BASELINE_REF (unpublished, disposable)" "$baseline")''')
b.rep('''    g_json=$(build "$good" v0.1.0-alpha.81)
    d_json=$(build "$defective" v0.1.0-alpha.81)
    s_json= r_json= startcheck= realstart=
    b_seq=80 c_seq=81 b_parent=$tag_commit''',
      '''    if [[ $BASELINE_REF == v0.1.0-alpha.81 ]]; then
        c_version=v0.1.0-alpha.82 b_seq=81 c_seq=82 b_parent=
    else
        c_version=v0.1.0-alpha.81 b_seq=80 c_seq=81 b_parent=$tag_commit
    fi
    g_json=$(build "$good" "$c_version")
    d_json=$(build "$defective" "$c_version")
    s_json= r_json= startcheck= realstart=''')
b.rep('''    document["baseline_ref"] = {"ref": ref, "tag_commit": tag, "patched_files": list(module.BASELINE_REF_PATCHED),
                                "proof": proof}
    document["provenance"] = (f"baseline: the published {ref} tree ({tag}) plus the D-027 acceptance-license seam only; "
                              "candidates: unpublished disposable fixture commits over the source labelled "
                              "v0.1.0-alpha.81; acceptance-license panels; signed only by the per-lab fixture key at "
                              "run time; not a release")''',
      '''    patched = list(module.baseline_ref_patched(ref))
    document["baseline_ref"] = {"ref": ref, "tag_commit": tag, "patched_files": patched, "proof": proof}
    document["provenance"] = (f"baseline: the published {ref} tree ({tag}) "
                              + ("plus the D-027 acceptance-license seam only; " if patched else
                                 "unchanged (the tag commit itself; it carries the acceptance-license seam); ")
                              + "candidates: unpublished disposable fixture commits over the source labelled "
                              f"{document['good']['version']}; acceptance-license panels; signed only by the per-lab "
                              "fixture key at run time; not a release")''')
b.save()
print('ok')
