// Package tui shows progress while a run lasts (a spinner for one probe, a bar for a matrix);
// the result is printed afterwards by the same code as a run without a terminal.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/shoootyou/litegauge/internal/apperr"
	"github.com/shoootyou/litegauge/internal/messages"
)

type stepMsg struct{}
type finishedMsg struct{}

type model struct {
	spin        spinner.Model
	bar         progress.Model
	label       string // spinner text; empty means a progress bar, which never ticks
	done, total int
	interrupted bool
}

// Spinner is the model of a single probe.
func Spinner(modelID, key string) tea.Model {
	return model{spin: spinner.New(spinner.WithSpinner(spinner.MiniDot)), label: fmt.Sprintf(messages.Labels.Spinner, modelID, key), total: 1}
}

// Progress is the model of a matrix: a bar that redraws on each finished cell.
func Progress(total int, noColor bool) tea.Model {
	bar := progress.New(progress.WithFillCharacters('█', '░'), progress.WithoutPercentage())
	if noColor {
		bar.FullColor, bar.EmptyColor = nil, nil
	}
	return model{bar: bar, total: total}
}

func (m model) Init() tea.Cmd {
	if m.label == "" {
		return nil
	}
	return m.spin.Tick
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.label == "" { // a zero tick is accepted by any spinner id: the bar must not reschedule it
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case stepMsg:
		m.done++
	case finishedMsg:
		return m, tea.Quit
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.interrupted = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	if m.label != "" {
		return tea.NewView(m.spin.View() + " " + m.label)
	}
	return tea.NewView(fmt.Sprintf("%s %d/%d", m.bar.ViewAs(float64(m.done)/float64(max(1, m.total))), m.done, m.total))
}

// Run shows the work in a Bubble Tea program. work runs the requests and calls step once per finished
// cell; a panic in it is raised again here once the program has ended.
func Run(ctx context.Context, in io.Reader, out io.Writer, m tea.Model, work func(ctx context.Context, step func())) error {
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	prog := tea.NewProgram(m, tea.WithContext(wctx), tea.WithInput(in), tea.WithOutput(out))
	crash := make(chan any, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				crash <- r
			}
			prog.Send(finishedMsg{})
		}()
		work(wctx, func() { prog.Send(stepMsg{}) })
	}()
	final, err := prog.Run()
	cancel()
	quit, _ := final.(model)
	if ctx.Err() != nil || errors.Is(err, tea.ErrInterrupted) || (err == nil && quit.interrupted) {
		return apperr.ErrInterrupted
	}
	select {
	case r := <-crash:
		panic(r)
	default:
	}
	if err != nil {
		return apperr.Fatal(messages.Err.Internal)
	}
	return nil
}
