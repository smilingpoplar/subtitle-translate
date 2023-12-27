package main

import (
	"fmt"
	"os"

	"github.com/asticode/go-astisub"
	"github.com/smilingpoplar/translate/translator/google"
	"github.com/spf13/cobra"
)

var (
	input  string
	output string
	tolang string
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
