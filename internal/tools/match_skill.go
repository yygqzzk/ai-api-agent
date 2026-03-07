package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MatchSkillTool struct {
	skillDir string
}

func NewMatchSkillTool(skillDir string) *MatchSkillTool {
	return &MatchSkillTool{skillDir: skillDir}
}

func (t *MatchSkillTool) Name() string {
	return "match_skill"
}

func (t *MatchSkillTool) Description() string {
	return "匹配预定义技能模板"
}

func (t *MatchSkillTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}}}`)
}

func (t *MatchSkillTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	_ = ctx
	var req MatchSkillArgs
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode match_skill args: %w", err)
	}
	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}

	skills, err := loadSkills(t.skillDir)
	if err != nil {
		return nil, err
	}
	best, score := matchBestSkill(req.Query, skills)
	return MatchSkillResult{
		Skill: best,
		Score: score,
	}, nil
}

func loadSkills(dir string) ([]SkillTemplate, error) {
	// os.ReadDir 会返回目录项列表。
	// 相比旧的 ioutil.ReadDir，它直接返回 []os.DirEntry，拿文件名和 IsDir 更方便。
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}
	out := make([]SkillTemplate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !(strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")) {
			continue
		}
		path := filepath.Join(dir, name)
		// 这里再用 os.ReadFile 把单个技能文件读成 []byte，后面会转成字符串做简单解析。
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read skill file %s: %w", path, err)
		}
		skill := parseSkillYAML(string(body))
		skill.File = path
		out = append(out, skill)
	}
	return out, nil
}

func parseSkillYAML(body string) SkillTemplate {
	lines := strings.Split(body, "\n")
	skill := SkillTemplate{}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "name:"):
			skill.Name = trimQuotes(strings.TrimSpace(strings.TrimPrefix(trim, "name:")))
		case strings.HasPrefix(trim, "description:"):
			skill.Description = trimQuotes(strings.TrimSpace(strings.TrimPrefix(trim, "description:")))
		case strings.HasPrefix(trim, "tags:"):
			raw := strings.TrimSpace(strings.TrimPrefix(trim, "tags:"))
			raw = strings.TrimPrefix(raw, "[")
			raw = strings.TrimSuffix(raw, "]")
			parts := strings.Split(raw, ",")
			for _, part := range parts {
				tag := trimQuotes(strings.TrimSpace(part))
				if tag != "" {
					skill.Tags = append(skill.Tags, strings.ToLower(tag))
				}
			}
		}
	}
	return skill
}

func matchBestSkill(query string, skills []SkillTemplate) (SkillTemplate, int) {
	tokens := strings.Fields(strings.ToLower(query))
	best := SkillTemplate{}
	bestScore := 0
	for _, skill := range skills {
		score := 0
		for _, tag := range skill.Tags {
			for _, tk := range tokens {
				if strings.Contains(tag, tk) || strings.Contains(tk, tag) {
					score += 3
				}
			}
		}
		desc := strings.ToLower(skill.Description)
		for _, tk := range tokens {
			if strings.Contains(desc, tk) {
				score++
			}
		}
		if score > bestScore {
			best = skill
			bestScore = score
		}
	}
	return best, bestScore
}

func trimQuotes(s string) string {
	return strings.Trim(strings.TrimSpace(s), "\"")
}
