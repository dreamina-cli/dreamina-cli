package mcp

import "strings"

// This file captures reflected types that appear in the binary in addition to
// the main request structs defined in client.go.

type HistoryItem struct {
	SubmitID        string               `json:"submit_id,omitempty"`
	HistoryID       string               `json:"history_id,omitempty"`
	HistoryRecordID string               `json:"history_record_id,omitempty"`
	TaskID          string               `json:"task_id,omitempty"`
	Status          string               `json:"status,omitempty"`
	QueueStatus     string               `json:"queue_status,omitempty"`
	QueueLength     int                  `json:"queue_length,omitempty"`
	QueueIdx        int                  `json:"queue_idx,omitempty"`
	QueuePriority   int                  `json:"priority,omitempty"`
	QueueProgress   int                  `json:"queue_progress,omitempty"`
	QueueDebugInfo  string               `json:"debug_info,omitempty"`
	ImageURL        string               `json:"image_url,omitempty"`
	VideoURL        string               `json:"video_url,omitempty"`
	Images          []*HistoryImage      `json:"images,omitempty"`
	Videos          []*HistoryVideo      `json:"videos,omitempty"`
	Details         []*HistoryItemDetail `json:"details,omitempty"`
	Queue           *QueueInfo           `json:"queue,omitempty"`
	Raw             map[string]any       `json:"-"`
}

type HistoryTask struct {
	SubmitID    string `json:"submit_id,omitempty"`
	HistoryID   string `json:"history_id,omitempty"`
	Status      string `json:"status,omitempty"`
	QueueStatus string `json:"queue_status,omitempty"`
}

type HistoryItemDetail struct {
	HistoryRecordID string `json:"history_record_id,omitempty"`
	Status          string `json:"status,omitempty"`
	QueueStatus     string `json:"queue_status,omitempty"`
	ImageURL        string `json:"image_url,omitempty"`
	VideoURL        string `json:"video_url,omitempty"`
}

type HistoryImage struct {
	URL      string `json:"url,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Origin   string `json:"origin,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

type HistoryImageInfo struct {
	ImageURL string `json:"image_url,omitempty"`
	Origin   string `json:"origin,omitempty"`
}

type HistoryVideo struct {
	URL       string           `json:"url,omitempty"`
	VideoURL  string           `json:"video_url,omitempty"`
	CoverURL  string           `json:"cover_url,omitempty"`
	FPS       int              `json:"fps,omitempty"`
	Width     int              `json:"width,omitempty"`
	Height    int              `json:"height,omitempty"`
	Format    string           `json:"format,omitempty"`
	Duration  float64          `json:"duration,omitempty"`
	Resources []*VideoResource `json:"resources,omitempty"`
}

type VideoResource struct {
	URL      string `json:"url,omitempty"`
	VideoURL string `json:"video_url,omitempty"`
	Type     string `json:"type,omitempty"`
}

type QueueInfo struct {
	QueueStatus string `json:"queue_status,omitempty"`
	QueueLength int    `json:"queue_length,omitempty"`
	QueueIdx    int    `json:"queue_idx,omitempty"`
	Priority    int    `json:"priority,omitempty"`
	Progress    int    `json:"progress,omitempty"`
	DebugInfo   string `json:"debug_info,omitempty"`
}

func (h *HistoryItem) GetStatus() string {
	if h == nil {
		return ""
	}
	if status := strings.TrimSpace(h.Status); status != "" {
		return status
	}
	return strings.TrimSpace(h.QueueStatus)
}

func (h *HistoryItem) GetSubmitID() string {
	if h == nil {
		return ""
	}
	if submitID := strings.TrimSpace(h.SubmitID); submitID != "" {
		return submitID
	}
	return strings.TrimSpace(h.HistoryRecordID)
}

func (h *HistoryItem) GetHistoryID() string {
	if h == nil {
		return ""
	}
	if historyID := strings.TrimSpace(h.HistoryID); historyID != "" {
		return historyID
	}
	if historyID := strings.TrimSpace(h.HistoryRecordID); historyID != "" {
		return historyID
	}
	return strings.TrimSpace(h.SubmitID)
}

func (h *HistoryItem) View() map[string]any {
	if h == nil {
		return nil
	}
	out := cloneAnyMap(h.Raw)
	if out == nil {
		out = map[string]any{}
	}
	if value := strings.TrimSpace(h.SubmitID); value != "" {
		out["submit_id"] = value
	}
	if value := strings.TrimSpace(h.HistoryID); value != "" {
		out["history_id"] = value
	}
	if value := strings.TrimSpace(h.HistoryRecordID); value != "" {
		out["history_record_id"] = value
	}
	if value := strings.TrimSpace(h.TaskID); value != "" {
		out["task_id"] = value
	}
	if value := strings.TrimSpace(h.Status); value != "" {
		out["status"] = value
	}
	if value := strings.TrimSpace(h.QueueStatus); value != "" {
		out["queue_status"] = value
	}
	if h.Queue != nil || h.QueueLength > 0 {
		out["queue_length"] = h.QueueLength
	}
	if h.Queue != nil || h.QueueIdx > 0 {
		out["queue_idx"] = h.QueueIdx
	}
	if h.QueuePriority > 0 {
		out["priority"] = h.QueuePriority
	}
	if h.QueueProgress > 0 {
		out["progress"] = h.QueueProgress
	}
	if value := strings.TrimSpace(h.QueueDebugInfo); value != "" {
		out["debug_info"] = value
	}
	if value := strings.TrimSpace(h.ImageURL); value != "" {
		out["image_url"] = value
	}
	if value := strings.TrimSpace(h.VideoURL); value != "" {
		out["video_url"] = value
	}
	if len(h.Images) > 0 {
		out["images"] = historyImagesView(h.Images)
	}
	if len(h.Videos) > 0 {
		out["videos"] = historyVideosView(h.Videos)
	}
	if len(h.Details) > 0 {
		out["details"] = historyDetailsView(h.Details)
	}
	if h.Queue != nil {
		queueView := map[string]any{
			"queue_status": strings.TrimSpace(h.Queue.QueueStatus),
			"queue_length": h.Queue.QueueLength,
			"queue_idx":    h.Queue.QueueIdx,
		}
		if h.Queue.Priority > 0 {
			queueView["priority"] = h.Queue.Priority
		}
		if h.Queue.Progress > 0 {
			queueView["progress"] = h.Queue.Progress
		}
		if value := strings.TrimSpace(h.Queue.DebugInfo); value != "" {
			queueView["debug_info"] = value
		}
		out["queue"] = queueView
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func historyImagesView(images []*HistoryImage) []map[string]any {
	out := make([]map[string]any, 0, len(images))
	for _, item := range images {
		if item == nil {
			continue
		}
		url := strings.TrimSpace(item.URL)
		if url == "" {
			url = strings.TrimSpace(item.ImageURL)
		}
		if url == "" {
			continue
		}
		view := map[string]any{
			"url":  url,
			"type": "image",
		}
		view["image_url"] = url
		if origin := strings.TrimSpace(item.Origin); origin != "" {
			view["origin"] = origin
		}
		if item.Width > 0 {
			view["width"] = item.Width
		}
		if item.Height > 0 {
			view["height"] = item.Height
		}
		out = append(out, view)
	}
	return out
}

func historyVideosView(videos []*HistoryVideo) []map[string]any {
	out := make([]map[string]any, 0, len(videos))
	for _, item := range videos {
		if item == nil {
			continue
		}
		url := strings.TrimSpace(item.URL)
		if url == "" {
			url = strings.TrimSpace(item.VideoURL)
		}
		if url == "" {
			continue
		}
		view := map[string]any{
			"url":  url,
			"type": "video",
		}
		view["video_url"] = url
		if coverURL := strings.TrimSpace(item.CoverURL); coverURL != "" {
			view["cover_url"] = coverURL
		}
		if item.FPS > 0 {
			view["fps"] = item.FPS
		}
		if item.Width > 0 {
			view["width"] = item.Width
		}
		if item.Height > 0 {
			view["height"] = item.Height
		}
		if format := strings.TrimSpace(item.Format); format != "" {
			view["format"] = format
		}
		if item.Duration > 0 {
			view["duration"] = item.Duration
		}
		out = append(out, view)
	}
	return out
}

func historyDetailsView(details []*HistoryItemDetail) []map[string]any {
	out := make([]map[string]any, 0, len(details))
	for _, item := range details {
		if item == nil {
			continue
		}
		view := map[string]any{}
		if value := strings.TrimSpace(item.HistoryRecordID); value != "" {
			view["history_record_id"] = value
		}
		if value := strings.TrimSpace(item.Status); value != "" {
			view["status"] = value
		}
		if value := strings.TrimSpace(item.QueueStatus); value != "" {
			view["queue_status"] = value
		}
		if value := strings.TrimSpace(item.ImageURL); value != "" {
			view["image_url"] = value
		}
		if value := strings.TrimSpace(item.VideoURL); value != "" {
			view["video_url"] = value
		}
		if len(view) == 0 {
			continue
		}
		out = append(out, view)
	}
	return out
}
