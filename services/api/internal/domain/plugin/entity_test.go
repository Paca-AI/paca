package plugindom

import "testing"

func TestPluginManifestValidate_Skills(t *testing.T) {
	base := func(skills *SkillsManifest) PluginManifest {
		return PluginManifest{
			ID:     "com.paca.example",
			Skills: skills,
		}
	}

	tests := []struct {
		name    string
		skills  *SkillsManifest
		wantErr bool
	}{
		{
			name:   "nil skills is valid",
			skills: nil,
		},
		{
			name: "valid skills block",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
				Names:   []string{"paca-pr-review", "paca-changelog"},
			},
		},
		{
			name: "missing base url",
			skills: &SkillsManifest{
				Names: []string{"paca-pr-review"},
			},
			wantErr: true,
		},
		{
			name: "no names",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
			},
			wantErr: true,
		},
		{
			name: "duplicate name",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
				Names:   []string{"paca-pr-review", "paca-pr-review"},
			},
			wantErr: true,
		},
		{
			name: "invalid name pattern",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
				Names:   []string{"PR_Review"},
			},
			wantErr: true,
		},
		{
			name: "missing paca- prefix",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
				Names:   []string{"pr-review"},
			},
			wantErr: true,
		},
		{
			name: "reserved trigger name",
			skills: &SkillsManifest{
				BaseURL: "/plugins-skills/com.paca.example",
				Names:   []string{"paca-trigger-chat"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := base(tt.skills).Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
