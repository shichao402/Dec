package registry

import (
	"context"
	"strings"
)

func ListRemoteTags(ctx context.Context, url string, env []string) ([]string, error) {
	out, err := GitEnv(ctx, "", env, "ls-remote", "--tags", url)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ref := fields[len(fields)-1]
		const prefix = "refs/tags/"
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		tag := strings.TrimPrefix(ref, prefix)
		if strings.HasSuffix(tag, "^{}") {
			continue
		}
		tags = append(tags, tag)
	}
	return tags, nil
}
