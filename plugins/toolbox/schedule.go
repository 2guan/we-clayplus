// 文件名：schedule.go
// 主要功能：校验并计算五段 Cron 表达式，支持列表、区间和步长，按指定时区逐分钟调度。
// 作者：2guan
package toolbox

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Schedule struct {
	Fields          [5]map[int]bool
	dayAny, weekAny bool
}

func ParseSchedule(raw string) (Schedule, error) {
	s := Schedule{}
	parts := strings.Fields(raw)
	if len(parts) != 5 {
		return s, fmt.Errorf("请输入五段 Cron：分 时 日 月 周")
	}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, part := range parts {
		s.Fields[i] = map[int]bool{}
		for _, piece := range strings.Split(part, ",") {
			step := 1
			pair := strings.Split(piece, "/")
			if len(pair) > 2 {
				return s, fmt.Errorf("Cron 步长无效")
			}
			if len(pair) == 2 {
				n, e := strconv.Atoi(pair[1])
				if e != nil || n < 1 || n > limits[i][1]+1 {
					return s, fmt.Errorf("Cron 步长无效")
				}
				step = n
			}
			low, high := limits[i][0], limits[i][1]
			if pair[0] != "*" {
				r := strings.Split(pair[0], "-")
				if len(r) > 2 {
					return s, fmt.Errorf("Cron 区间无效")
				}
				var e error
				low, e = strconv.Atoi(r[0])
				if e != nil {
					return s, fmt.Errorf("Cron 数值无效")
				}
				high = low
				if len(pair) == 2 {
					high = limits[i][1]
				}
				if len(r) == 2 {
					high, e = strconv.Atoi(r[1])
					if e != nil {
						return s, fmt.Errorf("Cron 数值无效")
					}
				}
			}
			if low < limits[i][0] || high > limits[i][1] || high < low {
				return s, fmt.Errorf("Cron 超出允许范围（周日为 0）")
			}
			for n := low; n <= high; n += step {
				s.Fields[i][n] = true
			}
		}
	}
	s.dayAny = strings.HasPrefix(parts[2], "*")
	s.weekAny = strings.HasPrefix(parts[4], "*")
	return s, nil
}
func (s Schedule) Match(t time.Time) bool {
	day, week := s.Fields[2][t.Day()], s.Fields[4][int(t.Weekday())]
	date := day && week
	if !s.dayAny && !s.weekAny {
		date = day || week
	}
	return s.Fields[0][t.Minute()] && s.Fields[1][t.Hour()] && s.Fields[3][int(t.Month())] && date
}
func (s Schedule) Next(t time.Time) (time.Time, error) {
	t = t.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if s.Match(t) {
			return t, nil
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("五年内没有有效执行日期")
}
