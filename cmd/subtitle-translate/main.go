package main

import (
	"fmt"
	"os"
	"time"

	"github.com/asticode/go-astisub"
	"github.com/smilingpoplar/translate/translator/google"
	"github.com/spf13/cobra"
)

var (
	input  string
	output string
	tolang string
	align  bool
	biling bool
	proxy  string
)

func main() {
	var cmd = &cobra.Command{
		Short:                 "translate subtitle file",
		Use:                   "subtitle-translate -i input.srt -o output.srt -b",
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			if err := translate(args); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		},
	}
	cmd.Flags().StringVarP(&input, "input", "i", "", "required, input subtitle file, .srt or .vtt")
	cmd.MarkFlagRequired("input")
	cmd.Flags().StringVarP(&output, "output", "o", "", "required, output subtitle file, .srt or .vtt")
	cmd.MarkFlagRequired("output")
	cmd.Flags().StringVarP(&tolang, "tolang", "t", "zh-CN", "target language")
	cmd.Flags().BoolVarP(&align, "align", "a", true, "align subtitle sentences, -a=false to disable")
	cmd.Flags().BoolVarP(&biling, "biling", "b", false, "bilingual subtitle")
	cmd.Flags().StringVarP(&proxy, "proxy", "p", "", `http or socks5 proxy,
eg. http://127.0.0.1:7890 or socks5://127.0.0.1:7890`)

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func translate(args []string) error {
	subs, err := astisub.OpenFile(input)
	if err != nil {
		return err
	}

	if align {
		alignSentences(subs)
	}

	texts := make([]string, 0, len(subs.Items))
	for _, item := range subs.Items {
		texts = append(texts, item.String())
	}

	g, err := google.New(google.WithProxy(proxy))
	if err != nil {
		return err
	}
	trans, err := g.Translate(texts, tolang)
	if err != nil {
		return err
	}

	for i, item := range subs.Items {
		var lines []astisub.Line
		if biling {
			lines = item.Lines
		}
		item.Lines = append(lines, astisub.Line{Items: []astisub.LineItem{{Text: trans[i]}}})
	}
	return subs.Write(output)
}

type Frag struct {
	startAt time.Duration
	endAt   time.Duration
	text    string
}

func alignSentences(subs *astisub.Subtitles) {
	frags := make([]Frag, 0, len(subs.Items))
	for _, item := range subs.Items {
		frags = append(frags, Frag{
			startAt: item.StartAt,
			endAt:   item.EndAt,
			text:    item.String(),
		})
	}
	frags = alignFrags(frags)

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
