package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/YashubuStudio/filetext"
)

const version = "0.2.0"

func main() {
	os.Exit(run())
}

func run() int {
	var selection filetext.Selection
	var outputPath string
	var formatName string
	var listFormats bool
	var detectOnly bool
	var showVersion bool
	var noHeader bool

	flag.StringVar(&formatName, "format", "auto", "force a parser, for example text, docx, email, or zip")
	flag.BoolVar(&detectOnly, "detect", false, "print the detected format without extracting")
	flag.StringVar(&selection.Lines, "lines", "", "1-based ranges: 1-20,40,80-")
	flag.StringVar(&selection.Match, "match", "", "keep lines matching this regular expression")
	flag.StringVar(&selection.Exclude, "exclude", "", "drop lines matching this regular expression")
	flag.IntVar(&selection.Before, "before", 0, "include N lines before each match")
	flag.IntVar(&selection.After, "after", 0, "include N lines after each match")
	flag.IntVar(&selection.Head, "head", 0, "return the first N selected lines")
	flag.IntVar(&selection.Tail, "tail", 0, "return the last N selected lines")
	flag.StringVar(&selection.From, "from", "", "start at the first line containing this text")
	flag.StringVar(&selection.To, "to", "", "stop after the first line containing this text")
	flag.IntVar(&selection.MaxChars, "max-chars", 0, "limit output to N Unicode characters")
	flag.BoolVar(&selection.IgnoreCase, "ignore-case", false, "ignore case in match, exclude, from, and to")
	flag.StringVar(&outputPath, "output", "", "write to a file instead of stdout")
	flag.BoolVar(&noHeader, "no-header", false, "omit file headings when reading multiple files")
	flag.BoolVar(&listFormats, "list-formats", false, "list recognized extensions")
	flag.BoolVar(&showVersion, "version", false, "print the version")
	flag.Usage = usage
	os.Args = append([]string{os.Args[0]}, interspersedArgs(os.Args[1:])...)
	flag.Parse()

	if showVersion {
		fmt.Println("filetext " + version)
		return 0
	}
	if listFormats {
		fmt.Println(strings.Join(filetext.SupportedExtensions(), "\n"))
		return 0
	}
	paths := flag.Args()
	if len(paths) == 0 {
		usage()
		return 2
	}
	if detectOnly {
		for _, path := range paths {
			format, err := filetext.DetectFormat(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "filetext: %s: %v\n", path, err)
				return 1
			}
			if len(paths) == 1 {
				fmt.Println(format)
			} else {
				fmt.Printf("%s\t%s\n", path, format)
			}
		}
		return 0
	}

	format, err := filetext.NormalizeFormat(formatName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var output io.Writer = os.Stdout
	var outputFile *os.File
	if outputPath != "" {
		outputFile, err = os.Create(outputPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "filetext:", err)
			return 1
		}
		defer outputFile.Close()
		output = outputFile
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	opts := filetext.Options{Format: format, Selection: selection}
	for index, path := range paths {
		if len(paths) > 1 && !noHeader {
			if index > 0 {
				fmt.Fprintln(output)
			}
			fmt.Fprintf(output, "===== %s =====\n", path)
		}
		if err := filetext.Write(ctx, path, output, opts); err != nil {
			fmt.Fprintf(os.Stderr, "filetext: %s: %v\n", path, err)
			return 1
		}
	}
	return 0
}

func interspersedArgs(args []string) []string {
	booleanFlags := map[string]bool{
		"detect":       true,
		"ignore-case":  true,
		"no-header":    true,
		"list-formats": true,
		"version":      true,
	}
	options := make([]string, 0, len(args))
	paths := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			paths = append(paths, args[index+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			paths = append(paths, arg)
			continue
		}
		options = append(options, arg)
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		if !booleanFlags[name] && !strings.Contains(arg, "=") && index+1 < len(args) {
			index++
			options = append(options, args[index])
		}
	}
	return append(options, paths...)
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintln(out, "Usage: filetext [options] FILE [FILE...]")
	fmt.Fprintln(out, "Extract readable text from text, Office, PDF, email, ebook, archive, and structured files.")
	fmt.Fprintln(out)
	flag.PrintDefaults()
}
