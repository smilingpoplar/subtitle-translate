package main

import (
	"fmt"
	"os"
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
	input   string
	output  string
	tolang  string
	align   bool
	biling  bool
	service string
	envfile string
	fixfile string
	proxy   string
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
	cmd.Flags().BoolVarP(&align, "align", "a", true, "align subtitle sentences, -a=false to disable")
	cmd.Flags().BoolVarP(&biling, "biling", "b", false, "bilingual subtitle")
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
	util.ApplyTranslationFixes(translated, fixes)

	fragsToSubs(frags, subs)
	for i, item := range subs.Items {
		var lines []astisub.Line
		if biling {
			lines = item.Lines
		}
		item.Lines = append(lines, astisub.Line{Items: []astisub.LineItem{{Text: translated[i]}}})
	}
	return subs.Write(output)
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

func fragsToSubs(frags []Frag, subs *astisub.Subtitles) {
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
				// frags[i]合并到prev中
				prev.endAt = frags[i].endAt
				prev.text = prev.text + " " + frags[i].text
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
