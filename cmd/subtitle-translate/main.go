package main

import (
	"fmt"
	"os"
	"strings"
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
	items := subs.Items
	subs.Items = nil

	frags := []Frag{}
	for _, item := range items {
		frags = append(frags, Frag{
			startAt: item.StartAt,
			endAt:   item.EndAt,
			text:    item.String(),
		})
	}

	var prev *Frag
	for i := 0; i < len(frags); {
		str := frags[i].text
		idx := strings.IndexAny(str, ".?!")
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
			// output
			subs.Items = append(subs.Items, &astisub.Item{
				StartAt: newFrag.startAt,
				EndAt:   newFrag.endAt,
				Lines: []astisub.Line{
					{Items: []astisub.LineItem{{Text: newFrag.text}}},
				},
			})

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
		// output
		subs.Items = append(subs.Items, &astisub.Item{
			StartAt: prev.startAt,
			EndAt:   prev.endAt,
			Lines: []astisub.Line{
				{Items: []astisub.LineItem{{Text: prev.text}}},
			},
		})
	}
}
