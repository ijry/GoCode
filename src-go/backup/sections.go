package backup

import "strings"

// The single table of YoYoCode-owned data sections a backup packs. Mirrors
// upstream sections.rs: an ALLOWLIST, so a future unexpected directory under the
// data home is never swept in. Live-path resolution is done by the CALLER (the
// handler knows the port's YOYOCODE_HOME / DataDir layout); the engine only
// iterates this table.

// SectionKind is a directory tree or a single file.
type SectionKind int

const (
	SectionDir SectionKind = iota
	SectionFile
)

// SectionPolicy is how the restore swap treats the section (used by the restore
// engine; recorded here for parity).
type SectionPolicy int

const (
	AlwaysReplace SectionPolicy = iota
	ReplaceIfPresent
)

// ManagedSection is one owned section. ID is the archive path prefix + snapshot
// subdir + the stable identifier in the manifest / pending marker.
type ManagedSection struct {
	ID     string
	Kind   SectionKind
	Policy SectionPolicy
}

// LegacySectionIDs are the sections the previous archive format managed
// implicitly; an archive without managedSections normalizes to these.
var LegacySectionIDs = []string{"uploads", "tokens.json", "preferences.json"}

// ManagedSections is the allowlist. IDs must be single safe path components and
// must not collide with the reserved db/ external/ prefixes.
var ManagedSections = []ManagedSection{
	{ID: "uploads", Kind: SectionDir, Policy: AlwaysReplace},
	{ID: "acp-transcripts", Kind: SectionDir, Policy: AlwaysReplace},
	{ID: "turn-timings", Kind: SectionDir, Policy: AlwaysReplace},
	{ID: "backgrounds", Kind: SectionDir, Policy: AlwaysReplace},
	{ID: "pets", Kind: SectionDir, Policy: AlwaysReplace},
	{ID: "skills", Kind: SectionDir, Policy: AlwaysReplace},
	{ID: "tokens.json", Kind: SectionFile, Policy: ReplaceIfPresent},
	{ID: "preferences.json", Kind: SectionFile, Policy: ReplaceIfPresent},
}

// ExternalSectionPrefix is the archive path prefix for optional external agent
// transcript trees (packing deferred in this port today).
const ExternalSectionPrefix = "external"

// AllSectionIDs returns every section id, in table order.
func AllSectionIDs() []string {
	out := make([]string, len(ManagedSections))
	for i, s := range ManagedSections {
		out[i] = s.ID
	}
	return out
}

// FindSection returns the managed section with id, or nil.
func FindSection(id string) *ManagedSection {
	for i := range ManagedSections {
		if ManagedSections[i].ID == id {
			return &ManagedSections[i]
		}
	}
	return nil
}

// NormalizeSectionIDs keeps only known ids, restoring table order (unknown ids
// from a newer archive are dropped, not guessed at).
func NormalizeSectionIDs(declared []string) []string {
	known := map[string]bool{}
	for _, d := range declared {
		known[d] = true
	}
	var out []string
	for _, s := range ManagedSections {
		if known[s.ID] {
			out = append(out, s.ID)
		}
	}
	return out
}

// IsExcludedSectionEntry excludes internal scaffolding from any packed section:
// upload staging (any `.tmp/` component) and any `.yoyocode`-prefixed name
// (restore staging / safety snapshots). Rebranded from upstream's `.codeg`.
func IsExcludedSectionEntry(rel string) bool {
	for _, comp := range strings.Split(strings.ReplaceAll(rel, "\\", "/"), "/") {
		if comp == ".tmp" || strings.HasPrefix(comp, ".yoyocode") {
			return true
		}
	}
	return false
}

// LiveRoots is the resolved live location of every managed section, resolved
// once so backup and restore agree.
type LiveRoots struct {
	paths map[string]string
}

// NewLiveRoots builds a LiveRoots by resolving each section id through pathFor.
func NewLiveRoots(pathFor func(id string) string) LiveRoots {
	m := make(map[string]string, len(ManagedSections))
	for _, s := range ManagedSections {
		m[s.ID] = pathFor(s.ID)
	}
	return LiveRoots{paths: m}
}

// Path returns the live path for a section id.
func (r LiveRoots) Path(id string) (string, bool) {
	p, ok := r.paths[id]
	return p, ok
}
