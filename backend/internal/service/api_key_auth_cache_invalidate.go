package service

import (
	"context"
	"log/slog"
)

// InvalidateAuthCacheByKey 清除指定 API Key 的认证缓存
func (s *APIKeyService) InvalidateAuthCacheByKey(ctx context.Context, key string) {
	if key == "" {
		return
	}
	cacheKey := s.authCacheKey(key)
	s.deleteAuthCache(ctx, cacheKey)
}

// InvalidateAuthCacheByUserID 清除用户相关的 API Key 认证缓存
func (s *APIKeyService) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	if userID <= 0 {
		return
	}
	keys, err := s.apiKeyRepo.ListKeysByUserID(ctx, userID)
	if err != nil {
		return
	}
	s.deleteAuthCacheByKeys(ctx, keys)
}

// InvalidateAuthCacheByGroupID 清除分组相关的 API Key 认证缓存
func (s *APIKeyService) InvalidateAuthCacheByGroupID(ctx context.Context, groupID int64) {
	if groupID <= 0 {
		return
	}
	keys, err := s.apiKeyRepo.ListKeysByGroupID(ctx, groupID)
	if err != nil {
		return
	}
	s.deleteAuthCacheByKeys(ctx, keys)
}

// InvalidateAuthCacheByTeamID 清除团队名下所有 API Key 的认证缓存（团队并发/RPM 上限变更后调用）。
func (s *APIKeyService) InvalidateAuthCacheByTeamID(ctx context.Context, teamID int64) {
	if teamID <= 0 {
		return
	}
	keys, err := s.apiKeyRepo.ListKeysByTeamID(ctx, teamID)
	if err != nil {
		slog.Error("ALERT: 团队鉴权缓存失效失败，团队并发/RPM 新上限生效可能延迟至快照 TTL", "team_id", teamID, "err", err)
		return
	}
	s.deleteAuthCacheByKeys(ctx, keys)
}

// DeleteTeamKeys 软删团队名下所有 API key 并失效其鉴权缓存（admin 强删团队时调用）。
func (s *APIKeyService) DeleteTeamKeys(ctx context.Context, teamID int64) error {
	if teamID <= 0 {
		return nil
	}
	keys, err := s.apiKeyRepo.DeleteByTeamID(ctx, teamID)
	if err != nil {
		return err
	}
	for _, k := range keys {
		s.InvalidateAuthCacheByKey(ctx, k)
	}
	return nil
}

func (s *APIKeyService) deleteAuthCacheByKeys(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		s.deleteAuthCache(ctx, s.authCacheKey(key))
	}
}
