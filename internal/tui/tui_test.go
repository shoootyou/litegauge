/**
 * @spec-handoff
 * @interface Spinner(model, key string) tea.Model; Progress(total int, noColor bool) tea.Model
 * @behavior
 *   - Spinner(...).Init() returns the spinner tick command
 *   - Progress(...).Init() returns nil, and Update of a spinner.TickMsg returns a nil command: the matrix bar redraws on steps, never on a timer
 * @edge-cases
 *   - A zero spinner.TickMsg is accepted by any spinner id, so a Progress model that kept a live spinner would reschedule itself
 * @see ../cli/tui_test.go
 */

package tui_test

import (
	"testing"

	"charm.land/bubbles/v2/spinner"

	"github.com/shoootyou/litegauge/internal/tui"
)

func TestM2_ProgressModelSchedulesNoTickWhileSpinnerDoes(t *testing.T) {
	if tui.Spinner("claude-sonnet-5-5", "env").Init() == nil {
		t.Fatal("Spinner Init returned nil: the single-probe spinner would never animate")
	}
	bar := tui.Progress(6, true)
	if cmd := bar.Init(); cmd != nil {
		t.Errorf("Progress Init returned a command: the matrix mode would tick forever")
	}
	if _, cmd := bar.Update(spinner.TickMsg{}); cmd != nil {
		t.Errorf("Progress Update of a spinner.TickMsg returned a command: the tick would reschedule itself")
	}
}
