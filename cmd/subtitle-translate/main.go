package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/asticode/go-astisub"
	"github.com/joho/godotenv"
	"github.com/smilingpoplar/translate/config"
	"github.com/smilingpoplar/translate/translator"
	"github.com/smilingpoplar/translate/util"
	"github.com/spf13/cobra"
)

var (
	input     string
	output    string
	tolang    string
	align     bool
	bilingual bool
	service   string
	envfile   string
	fixfile   string
	proxy     string
)

func main() {
	cmd := initCmd()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Short:                 "translate subtitle file",
		Use:                   "subtitle-translate -i input.srt -o output.srt -b",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initEnv(); err != nil {
				return err
			}
			return translate(args)
		},
	}

	cmd.Flags().StringVarP(&input, "input", "i", "", "required, input subtitle file, .srt or .vtt")
	cmd.MarkFlagRequired("input")
	cmd.Flags().StringVarP(&output, "output", "o", "", "required, output subtitle file, .srt or .vtt")
	cmd.MarkFlagRequired("output")
	cmd.Flags().StringVarP(&tolang, "tolang", "t", "zh-CN", "target language")
	cmd.Flags().BoolVarP(&align, "align", "a", false, "align subtitle sentences")
	cmd.Flags().BoolVarP(&bilingual, "bilingual", "b", false, "output both monolingual and bilingual subtitles")
	services := fmt.Sprintf("translate service, eg. %s", strings.Join(config.GetAllServiceNames(), ", "))
	cmd.Flags().StringVarP(&service, "service", "s", "google", services)
	cmd.Flags().StringVarP(&envfile, "envfile", "e", "", "env file, search .env upwards if not set")
	cmd.Flags().StringVarP(&fixfile, "fixfile", "f", "", "csv file to fix translation")
	cmd.Flags().StringVarP(&proxy, "proxy", "p", "", `http or socks5 proxy,
eg. http://127.0.0.1:7890 or socks5://127.0.0.1:7890`)

	return cmd
}

func initEnv() error {
	filename := envfile
	if filename == "" {
		filename = ".env"
	}

	path, err := util.FileExistsInParentDirs(filename)
	if err != nil { // 文件不存在
		if envfile != "" {
			return fmt.Errorf("error envfile: %w", err)
		}
		return nil
	}

	if err := godotenv.Load(path); err != nil {
		if envfile != "" {
			return fmt.Errorf("error loading envfile: %w", err)
		}
	}
	return nil
}

func translate(args []string) error {
	subs, err := astisub.OpenFile(input)
	if err != nil {
		return err
	}

	frags := subsToFrags(subs)
	if align {
		frags = alignFrags(frags)
		frags = splitByCommaIfTooLong(frags)
	}
	frags = fixFrags(frags)

	texts := make([]string, 0, len(frags))
	for _, frag := range frags {
		texts = append(texts, frag.text)
	}

	t, err := translator.GetTranslator(service, proxy)
	if err != nil {
		return err
	}

	fixes, err := util.LoadTranslationFixes(fixfile)
	if err != nil {
		return err
	}

	translated, err := t.Translate(texts, tolang)
	if err != nil {
		return err
	}
	if len(translated) != len(texts) {
		return fmt.Errorf("error translating subtitles: texts len %d, translated len %d", len(texts), len(translated))
	}
	util.ApplyTranslationFixes(translated, fixes)

	subs = fragsToSubs(frags, translated, false)
	if err := subs.Write(output); err != nil {
		return err
	}

	if bilingual { // 额外写入双语字幕
		output2 := bilingualOutputName(output)
		subs = fragsToSubs(frags, translated, true)
		if err := subs.Write(output2); err != nil {
			return err
		}
	}

	return nil
}

type Frag struct {
	startAt time.Duration
	endAt   time.Duration
	text    string
}

func subsToFrags(subs *astisub.Subtitles) []Frag {
	frags := make([]Frag, 0, len(subs.Items))
	for _, item := range subs.Items {
		frags = append(frags, Frag{
			startAt: item.StartAt,
			endAt:   item.EndAt,
			text:    getText(item),
		})
	}

	return frags
}

func getText(item *astisub.Item) string {
	var parts []string
	for _, line := range item.Lines {
		for _, lineItem := range line.Items {
			if lineItem.Text != "" {
				parts = append(parts, lineItem.Text)
			}
		}
	}
	return strings.Join(parts, " ")
}

func fragsToSubs(frags []Frag, translated []string, bilingual bool) *astisub.Subtitles {
	subs := astisub.NewSubtitles()
	subs.Items = make([]*astisub.Item, 0, len(frags))
	for _, frag := range frags {
		subs.Items = append(subs.Items, &astisub.Item{
			StartAt: frag.startAt,
			EndAt:   frag.endAt,
			Lines: []astisub.Line{
				{Items: []astisub.LineItem{{Text: frag.text}}},
			},
		})
	}

	for i, item := range subs.Items {
		var lines []astisub.Line
		if bilingual {
			lines = item.Lines
		}
		item.Lines = append(lines, astisub.Line{Items: []astisub.LineItem{{Text: translated[i]}}})
	}
	return subs
}

func bilingualOutputName(output string) string {
	ext := filepath.Ext(output)
	base := strings.TrimSuffix(output, ext)
	return base + ".dual" + ext
}

func fixFrags(frags []Frag) []Frag {
	// 起止时间相同的字幕合并到下一个字幕
	result := make([]Frag, 0, len(frags))
	pad := ""
	for _, frag := range frags {
		if frag.startAt == frag.endAt {
			pad += frag.text
			continue
		}

		text := frag.text
		if pad != "" {
			text = pad + text
			pad = ""
		}
		result = append(result, Frag{
			startAt: frag.startAt,
			endAt:   frag.endAt,
			text:    text,
		})
	}

	// 最后一个字幕是"-"，合并到前一个字幕
	if len(result) > 1 {
		last := &result[len(result)-1]
		if strings.TrimSpace(last.text) == "-" {
			prev := &result[len(result)-2]
			prev.endAt = last.endAt
			prev.text = prev.text + last.text
			result = result[:len(result)-1]
		}
	}

	return result
}

func alignFrags(frags []Frag) []Frag {
	result := []Frag{}
	var prev *Frag
	for i := 0; i < len(frags); {
		str := frags[i].text
		idx := indexAny(str, ".?!")
		if idx != -1 {
			ratio := float64(idx+1) / float64(len(str))
			duration := frags[i].endAt - frags[i].startAt
			cutAt := frags[i].startAt + time.Duration(float64(duration)*ratio)
			newFrag := Frag{frags[i].startAt, cutAt, str[:idx+1]}
			if prev != nil {
				newFrag.startAt = prev.startAt
				newFrag.text = prev.text + " " + newFrag.text
				prev = nil
			}
			result = append(result, newFrag)

			frags[i].startAt = cutAt
			frags[i].text = str[idx+1:]
		} else { // 没有找到
			if prev != nil {
				// 计算合并后的总时长
				if frags[i].endAt-prev.startAt > 8*time.Second {
					result = append(result, *prev)
					prev = &frags[i]
				} else {
					prev.endAt = frags[i].endAt
					prev.text = prev.text + " " + frags[i].text
				}
			} else {
				prev = &frags[i]
			}
			i++
		}
	}
	if prev != nil {
		result = append(result, *prev)
	}
	return result
}

func indexAny(s, chars string) int {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				if i < len(s)-1 && s[i+1] != ' ' {
					continue // 比如，s中的".x"不识别为句号
				}
				return i
			}
		}
	}
	return -1
}

func splitByCommaIfTooLong(frags []Frag) []Frag {
	const minLen, maxLen = 10, 80
	result := []Frag{}

	for _, frag := range frags {
		if len(frag.text) <= maxLen {
			result = append(result, frag)
			continue
		}

		parts := splitByComma(frag.text, minLen, maxLen)
		if len(parts) == 0 {
			result = append(result, frag)
			continue
		}

		// 按字节数比例分配时间
		totalBytes := 0
		for _, part := range parts {
			totalBytes += len(part)
		}

		duration := frag.endAt - frag.startAt
		startAt := frag.startAt

		for i, part := range parts {
			ratio := float64(len(part)) / float64(totalBytes)
			partDuration := time.Duration(float64(duration) * ratio)

			endAt := startAt + partDuration
			if i == len(parts)-1 {
				endAt = frag.endAt
			}

			result = append(result, Frag{
				startAt: startAt,
				endAt:   endAt,
				text:    part,
			})

			startAt = endAt
		}
	}

	return result
}

func splitByComma(text string, minLen, maxLen int) []string {
	parts := strings.Split(text, ",")
	if len(parts) <= 1 {
		return nil
	}

	// 清理和过滤空部分
	var cleanParts []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			cleanParts = append(cleanParts, part)
		}
	}

	if len(cleanParts) <= 1 {
		return nil
	}

	var result []string
	current := ""

	for i, part := range cleanParts {
		if current == "" {
			current = part
		} else {
			test := current + ", " + part
			// 尽量往长了拼，只有在超过maxLen时才分割
			if len(test) <= maxLen {
				current = test
			} else {
				// 当前片段已经很长，需要分割
				// 如果不是最后一个部分，添加逗号
				if i < len(cleanParts)-1 {
					result = append(result, current+",")
				} else {
					result = append(result, current)
				}
				current = part
			}
		}
	}

	if current != "" {
		result = append(result, current)
	}

	return result
}
