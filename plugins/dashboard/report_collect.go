// 文件名：report_collect.go
// 主要功能：增量缓存个人朋友圈与压缩缩略图，为报告提供固定资料版本。
// 作者：2guan
package dashboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
	"github.com/we-clay/we-clay/sdk/control"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

func (s *Server) collectReportMoments(ctx context.Context, id string, snap *reportSnapshot, refresh bool, info *InsightInfo) error {
	s.reportCacheMu.Lock()
	defer s.reportCacheMu.Unlock()
	key := reportKey("insight-cache", snap.Account, snap.Target)
	cache := momentAnalysisCache{Account: snap.Account, Target: snap.Target, Captured: snap.Captured}
	if !refresh {
		var old momentAnalysisCache
		if s.store.loadReportValue(key, &old) == nil && old.Account == snap.Account && old.Target == snap.Target {
			cache = old
			info.CacheHit = true
		}
	}
	count := snap.Range.Count
	seen := map[string]bool{}
	for _, m := range cache.Items {
		seen[m.ID] = true
	}
	cursors := map[string]bool{}
	for len(cache.Items) < count && !cache.Complete {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.reportProgress(id, fmt.Sprintf("正在读取朋友圈 · 已缓存 %d 条", len(cache.Items)))
		var page MomentPage
		if s.cfg.Mock {
			for _, m := range s.store.snapshot().Moments {
				if m.AuthorID == snap.Target {
					page.Items = append(page.Items, m)
				}
			}
		} else {
			if s.host == nil {
				return fmt.Errorf("微信未就绪，无法读取朋友圈")
			}
			result, err := s.host.Execute(ctx, control.Command{Operation: "moments-page", Target: snap.Target, ID: cache.Cursor})
			if err != nil {
				return fmt.Errorf("读取朋友圈失败：%w", err)
			}
			raw, e := json.Marshal(result)
			if e != nil {
				return e
			}
			if e = json.Unmarshal(raw, &page); e != nil {
				return fmt.Errorf("朋友圈分页格式无效")
			}
		}
		for _, m := range page.Items {
			if m.AuthorID == snap.Target && !seen[m.ID] && m.ID != "" {
				cache.Items = append(cache.Items, m)
				seen[m.ID] = true
			}
		}
		next := page.NextCursor
		cache.Complete = next == "" || next == cache.Cursor || cursors[next]
		cursors[next] = true
		cache.Cursor = next
		// 每页落盘；网络失败后重试从未完成的游标继续，不丢掉已获取资料。
		if err := s.store.saveReportValue(key, cache); err != nil {
			return err
		}
		info.Refresh = false
		s.updateTask(id, func(t *Task) {
			if t.Insight != nil {
				t.Insight.Refresh = false
			}
		})
	}
	if len(cache.Items) == 0 {
		return fmt.Errorf("这位好友当前没有可读取的朋友圈内容")
	}
	items := cache.Items[:min(count, len(cache.Items))]
	totalImages := 0
	for _, m := range items {
		totalImages += len(reportImageURLs(m))
	}

	snap.Captured = cache.Captured
	refs := []reportImageRef{}
	for _, m := range items {
		text := strings.Join(slices.DeleteFunc([]string{m.Content, m.Title, m.Link}, func(v string) bool { return v == "" }), "\n")
		if text == "" {
			text = "[图片动态，内容以附带缩略图为准]"
		}
		snap.Documents = append(snap.Documents, contextcleaner.Message{ID: m.ID, Session: snap.Target, Role: "user", Sender: m.Author, Source: "document", Kind: "text", Text: text, Time: m.Time})
		for i, src := range reportImageURLs(m) {
			refs = append(refs, reportImageRef{Moment: m.ID, Index: i, Source: src})
		}
	}
	imageCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]reportImageResult, len(refs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var done, totalBytes atomic.Int64
	var overBudget atomic.Bool
	side, quality := reportImageSize(totalImages)
	for range min(3, len(refs)) {
		wg.Go(func() {
			for i := range jobs {
				if imageCtx.Err() != nil {
					return
				}
				results[i] = s.cachedReportImage(imageCtx, snap.Account, refs[i], side, quality)
				if results[i].Image.Data != "" {
					data, e := base64.StdEncoding.DecodeString(results[i].Image.Data)
					if e != nil {
						results[i].Fatal = fmt.Errorf("缩略图缓存损坏，请更新资料")
					} else if totalBytes.Add(int64(len(data))) > 12<<20 {
						overBudget.Store(true)
						cancel()
					}
				}
				s.reportProgress(id, fmt.Sprintf("正在准备缩略图 · %d/%d", done.Add(1), totalImages))
			}
		})
	}
sendJobs:
	for i := range refs {
		select {
		case jobs <- i:
		case <-imageCtx.Done():
			break sendJobs
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if overBudget.Load() {
		return fmt.Errorf("缩略图总量超过 12 MB，请减少朋友圈条数")
	}
	for _, r := range results {
		if r.Fatal != nil {
			return r.Fatal
		}
		if r.Image.Data == "" {
			info.SkippedImages++
		} else {
			snap.Images = append(snap.Images, r.Image)
		}
	}

	return nil
}

type reportImageRef struct {
	Moment string
	Index  int
	Source string
}
type reportImageResult struct {
	Image ai.InputImage
	Fatal error
}

func (s *Server) cachedReportImage(ctx context.Context, account string, ref reportImageRef, side, quality int) reportImageResult {
	ik := reportKey("image", ref.Moment, fmt.Sprintf("%d:%d:%d:%s", ref.Index, side, quality, ref.Source))
	key := reportKey("insight-image", account, ik)
	var im ai.InputImage
	if s.store.loadReportValue(key, &im) == nil {
		return reportImageResult{Image: im}
	}
	var body []byte
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		body, err = s.reportImage(ctx, ref.Source)
		if err == nil || ctx.Err() != nil {
			break
		}
		if attempt == 0 {
			select {
			case <-ctx.Done():
				return reportImageResult{Fatal: ctx.Err()}
			case <-time.After(350 * time.Millisecond):
			}
		}
	}
	if err != nil {
		return reportImageResult{}
	}
	im, err = compressReportImage(body, side, quality)
	if err != nil {
		return reportImageResult{}
	}
	im.Label = fmt.Sprintf("朋友圈 %s 的缩略图 %d", ref.Moment, ref.Index+1)
	if err = s.store.saveReportValue(key, im); err != nil {
		return reportImageResult{Fatal: err}
	}
	return reportImageResult{Image: im}
}

func reportImageURLs(m Moment) []string {
	out := []string{}
	for _, src := range append(append([]string{}, m.Images...), m.Thumbnail) {
		if src != "" && !slices.Contains(out, src) {
			out = append(out, src)
		}
	}
	return out
}
func reportImageSize(count int) (int, int) {
	switch {
	case count > 96:
		return 100, 50
	case count > 48:
		return 160, 58
	case count > 24:
		return 256, 64
	case count > 8:
		return 384, 70
	default:
		return 512, 76
	}
}
func compressReportImage(body []byte, side, quality int) (ai.InputImage, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
		return ai.InputImage{}, fmt.Errorf("缩略图尺寸不受支持")
	}
	src, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return ai.InputImage{}, err
	}
	w, h := cfg.Width, cfg.Height
	if max(w, h) > side {
		w = max(1, w*side/max(cfg.Width, cfg.Height))
		h = max(1, h*side/max(cfg.Width, cfg.Height))
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var b bytes.Buffer
	if err = jpeg.Encode(&b, dst, &jpeg.Options{Quality: quality}); err != nil {
		return ai.InputImage{}, err
	}
	return ai.InputImage{MIME: "image/jpeg", Data: base64.StdEncoding.EncodeToString(b.Bytes())}, nil
}
func (s *Server) reportImage(ctx context.Context, src string) ([]byte, error) {
	if strings.HasPrefix(src, "/api/wechat-image?") {
		u, err := url.Parse(src)
		if err != nil {
			return nil, err
		}
		b, _, err := fetchWechatImage(ctx, u.Query().Get("url"), u.Query().Get("key"))
		return b, err
	}
	if _, ok := wechatImageURL(src); ok {
		b, _, err := fetchWechatImage(ctx, src, "")
		return b, err
	}
	if strings.HasPrefix(src, "/media/") {
		name := strings.TrimPrefix(src, "/media/")
		if name == "" || name != filepath.Base(name) {
			return nil, fmt.Errorf("图片路径无效")
		}
		f, err := os.Open(filepath.Join(s.cfg.DataDir, "media", name))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
		if len(b) > 8<<20 {
			return nil, fmt.Errorf("图片超过 8 MB")
		}
		return b, err
	}
	return nil, fmt.Errorf("图片来源不受支持")
}
