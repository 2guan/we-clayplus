// 文件名：content.go
// 主要功能：生成消息排行、中文词频、每日倒计时和菜单推荐；不访问账号或外部服务。
// 作者：2guan
package toolbox

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/go-ego/gse"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Count struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func Ranking(counts map[string]int, limit int) []Count {
	out := []Count{}
	for k, v := range counts {
		out = append(out, Count{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Name < out[j].Name
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

var urlRE = regexp.MustCompile(`https?://\S+`)
var wordSegmenter struct {
	sync.Mutex
	once sync.Once
	seg  gse.Segmenter
	err  error
}

// 使用内嵌中文词典，不在首次运行时下载数据。分词器串行复用；用户词典仅作用于本次输入。
func Words(texts []string, stop, dictionary string, limit int) ([]Count, error) {
	wordSegmenter.once.Do(func() {
		wordSegmenter.seg.SkipLog = true
		wordSegmenter.err = wordSegmenter.seg.LoadDictEmbed("zh")
		if wordSegmenter.err == nil {
			wordSegmenter.err = wordSegmenter.seg.LoadStopEmbed()
		}
	})
	if wordSegmenter.err != nil {
		return nil, fmt.Errorf("中文分词词典加载失败")
	}
	wordSegmenter.Lock()
	defer wordSegmenter.Unlock()
	split := func(r rune) bool { return unicode.IsSpace(r) || r == ',' || r == '，' }
	blocked := map[string]bool{}
	for _, w := range strings.FieldsFunc("这个,那个,我们,你们,他们,什么,怎么,然后,就是,但是,所以,已经,收到,好的,感觉,先把,"+stop, split) {
		blocked[strings.ToLower(w)] = true
	}
	custom := strings.FieldsFunc(dictionary, split)
	sort.SliceStable(custom, func(i, j int) bool { return len(custom[i]) > len(custom[j]) })
	var pattern *regexp.Regexp
	if len(custom) > 0 {
		quoted := []string{}
		for _, v := range custom {
			quoted = append(quoted, regexp.QuoteMeta(v))
		}
		pattern = regexp.MustCompile(strings.Join(quoted, "|"))
	}
	counts := map[string]int{}
	add := func(w string) {
		w = strings.TrimSpace(strings.ToLower(w))
		if len([]rune(w)) < 2 || blocked[w] || wordSegmenter.seg.IsStop(w) {
			return
		}
		letter := false
		for _, r := range w {
			if unicode.IsLetter(r) {
				letter = true
			}
		}
		if letter {
			counts[w]++
		}
	}
	cut := func(text string) {
		for _, w := range wordSegmenter.seg.Cut(text, true) {
			add(w)
		}
	}
	for _, text := range texts {
		if id, _ := Match(text); id != "" {
			continue
		}
		text = urlRE.ReplaceAllString(text, "")
		if pattern == nil {
			cut(text)
			continue
		}
		last := 0
		for _, span := range pattern.FindAllStringIndex(text, -1) {
			cut(text[last:span[0]])
			add(text[span[0]:span[1]])
			last = span[1]
		}
		cut(text[last:])
	}
	return Ranking(counts, limit), nil
}

func Meal(menu, exclude string) (string, error) {
	items := []string{}
	excluded := strings.FieldsFunc(exclude, func(r rune) bool { return r == ',' || r == '，' || r == '\n' })
	for _, line := range strings.Split(menu, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ok := true
		for _, x := range excluded {
			if strings.TrimSpace(x) != "" && strings.Contains(line, strings.TrimSpace(x)) {
				ok = false
			}
		}
		if ok {
			items = append(items, line)
		}
	}
	if len(items) == 0 {
		return "", fmt.Errorf("没有符合条件的菜品，请调整菜单或排除项")
	}
	n, e := rand.Int(rand.Reader, big.NewInt(int64(len(items))))
	if e != nil {
		return "", e
	}
	return "今天可以吃：" + items[n.Int64()], nil
}
func Daily(now time.Time, holidays string) (string, error) {
	dates := map[string]string{}
	if json.Unmarshal([]byte(holidays), &dates) != nil {
		return "", fmt.Errorf("节假日应为日期到名称的 JSON 对象")
	}
	text := now.Format("2006年01月02日") + " · 星期" + string([]rune("日一二三四五六")[now.Weekday()])
	days := (6 - int(now.Weekday()) + 7) % 7
	if now.Weekday() == time.Sunday {
		days = 0
	}
	if days == 0 {
		text += "\n今天是周末，留一点时间给自己。"
	} else {
		text += fmt.Sprintf("\n距离周末还有 %d 天", days)
	}
	keys := []string{}
	for date, name := range dates {
		d, e := time.ParseInLocation("2006-01-02", date, now.Location())
		if e != nil || strings.TrimSpace(name) == "" {
			return "", fmt.Errorf("节假日日期或名称无效")
		}
		if d.Format("2006-01-02") >= now.Format("2006-01-02") {
			keys = append(keys, date)
		}
	}
	sort.Strings(keys)
	for i, date := range keys {
		if i == 4 {
			break
		}
		d, _ := time.ParseInLocation("2006-01-02", date, now.Location())
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		n := int(d.Sub(today).Hours()/24 + 0.5)
		text += fmt.Sprintf("\n%s：还有 %d 天", dates[date], n)
	}
	return text, nil
}
