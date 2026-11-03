package ui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// benchFullOutputWidth pins the terminal width used by the task 111
// reproduction: a 1 MiB newline-heavy result cached ~57 MB at 100 columns
// under TrueColor because every rendered line was padded to the panel width.
const benchFullOutputWidth = 100

// benchFullOutputSizes cover a moderately large result and the 1 MiB retention
// ceiling.
var benchFullOutputSizes = []int{64 << 10, 1 << 20}

// benchFullOutputSink keeps the rendered string observable so the compiler
// cannot drop the call being measured.
var benchFullOutputSink string

// BenchmarkRenderToolFullOutput measures Ctrl+O full-mode rendering of
// multi-line results. rendered-B is the cached render size the fix bounds;
// bytes/op and allocs/op show whether the render itself allocates in proportion
// to the output or to width × lines.
func BenchmarkRenderToolFullOutput(b *testing.B) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	b.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	shapes := []struct {
		name string
		body func(n int) string
	}{
		{
			name: "newline-heavy",
			body: func(n int) string { return strings.Repeat("x\n", n/2) },
		},
		{
			name: "multiline",
			body: func(n int) string {
				const line = "build log line with a little detail\n"
				return strings.Repeat(line, n/len(line))
			},
		},
	}
	th := DefaultTheme()
	for _, shape := range shapes {
		for _, n := range benchFullOutputSizes {
			b.Run(shape.name+"/bytes="+strconv.Itoa(n), func(b *testing.B) {
				output := shape.body(n)
				item := Item{Kind: KindTool, Tool: &ToolBlock{
					Name:      "bash",
					Arguments: `{"command":"cat huge.log"}`,
					Output:    output,
					Done:      true,
				}}
				b.SetBytes(int64(len(output)))
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					benchFullOutputSink = RenderItem(th, item, benchFullOutputWidth, DefaultOutputLimit, ToolOutputFull)
				}
				b.StopTimer()
				b.ReportMetric(float64(len(benchFullOutputSink)), "rendered-B")
			})
		}
	}
}
