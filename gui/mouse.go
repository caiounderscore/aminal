package gui

import (
	"fmt"
	"math"

	"time"

	"github.com/go-gl/glfw/v3.2/glfw"
	"github.com/liamg/aminal/buffer"
	"github.com/liamg/aminal/terminal"
)

func (gui *GUI) glfwScrollCallback(w *glfw.Window, xoff float64, yoff float64) {

	if yoff > 0 {
		gui.terminal.ScreenScrollUp(1)
	} else {
		gui.terminal.ScreenScrollDown(1)
	}
}

func (gui *GUI) getHandCursor() *glfw.Cursor {
	if gui.handCursor == nil {
		gui.handCursor = glfw.CreateStandardCursor(glfw.HandCursor)
	}

	return gui.handCursor
}

func (gui *GUI) getArrowCursor() *glfw.Cursor {
	if gui.arrowCursor == nil {
		gui.arrowCursor = glfw.CreateStandardCursor(glfw.ArrowCursor)
	}

	return gui.arrowCursor
}

func (gui *GUI) mouseMoveCallback(w *glfw.Window, px float64, py float64) {

	x, y := gui.convertMouseCoordinates(px, py)

	if gui.mouseDown {
		if gui.terminal.GetMouseMode() == terminal.MouseModeButtonEvent {
			tx := int(x) + 1 // vt100 is 1 indexed
			ty := int(y) + 1
			gui.emitButtonEventToTerminal(tx, ty, glfw.MouseButtonLeft, nil, gui.mouseDownModifier)
		} else {
			gui.terminal.ActiveBuffer().ExtendSelection(x, y, false)
		}
	} else {

		hint := gui.terminal.ActiveBuffer().GetHintAtPosition(x, y)
		if hint != nil {
			gui.setOverlay(newAnnotation(hint))
		} else {
			gui.setOverlay(nil)
		}
	}

	if url := gui.terminal.ActiveBuffer().GetURLAtPosition(x, y); url != "" {
		w.SetCursor(gui.getHandCursor())
	} else {
		w.SetCursor(gui.getArrowCursor())
	}
}

func (gui *GUI) convertMouseCoordinates(px float64, py float64) (uint16, uint16) {
	scale := gui.scale()
	px = px / float64(scale)
	py = py / float64(scale)
	x := uint16(math.Floor((px - float64(gui.renderer.areaX)) / float64(gui.renderer.CellWidth())))
	y := uint16(math.Floor((py - float64(gui.renderer.areaY)) / float64(gui.renderer.CellHeight())))

	return x, y
}

func (gui *GUI) updateLeftClickCount(x uint16, y uint16) int {
	defer func() {
		gui.leftClickTime = time.Now()
		gui.prevLeftClickX = x
		gui.prevLeftClickY = y
	}()

	if gui.prevLeftClickX == x && gui.prevLeftClickY == y && time.Since(gui.leftClickTime) < time.Millisecond*500 {
		gui.leftClickCount++
		if gui.leftClickCount > 3 {
			gui.leftClickCount = 3
		}
	} else {
		gui.leftClickCount = 1
	}

	return gui.leftClickCount
}

func btnCode(button glfw.MouseButton, release bool, mod glfw.ModifierKey) (b byte, ok bool) {
	if release {
		b = 3
	} else {
		switch button {
		case glfw.MouseButton1:
			b = 0
		case glfw.MouseButton2:
			b = 1
		case glfw.MouseButton3:
			b = 2
		default:
			return 0, false
		}
	}

	if mod&glfw.ModShift > 0 {
		b |= 4
	}
	if mod&glfw.ModSuper > 0 {
		b |= 8
	}
	if mod&glfw.ModControl > 0 {
		b |= 16
	}
	return b, true
}

func (gui *GUI) mouseButtonCallback(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mod glfw.ModifierKey) {

	if gui.overlay != nil {
		if button == glfw.MouseButtonRight && action == glfw.Release {
			gui.setOverlay(nil)
		}
		return
	}

	// before we forward clicks on (below), we need to handle them locally for url clicking, text highlighting etc.
	x, y := gui.convertMouseCoordinates(w.GetCursorPos())
	tx := int(x) + 1 // vt100 is 1 indexed
	ty := int(y) + 1

	switch button {
	case glfw.MouseButtonLeft:
		if action == glfw.Press {
			gui.mouseDownModifier = mod
			gui.mouseDown = true

			if gui.terminal.GetMouseMode() != terminal.MouseModeButtonEvent {
				gui.handleSelectionButtonPress(x, y, mod)
			}
		} else if action == glfw.Release {
			gui.mouseDown = false

			if gui.terminal.GetMouseMode() != terminal.MouseModeButtonEvent {
				gui.handleSelectionButtonRelease(x, y)
			}
		}

	case glfw.MouseButtonRight:
		if gui.config.CopyAndPasteWithMouse && action == glfw.Press && gui.terminal.GetMouseMode() == terminal.MouseModeNone {
			str, err := gui.window.GetClipboardString()
			if err == nil {
				activeBuffer := gui.terminal.ActiveBuffer()
				activeBuffer.ClearSelection()
				_ = gui.terminal.Paste([]byte(str))
			}
		}
	}

	// https://www.xfree86.org/4.8.0/ctlseqs.html

	/*
		Parameters (such as pointer position and button number) for all mouse tracking escape sequences
		generated by xterm encode numeric parameters in a single character as value+32. For example,
		! specifies the value 1. The upper left character position on the terminal is denoted as 1,1.
	*/

	switch gui.terminal.GetMouseMode() {
	case terminal.MouseModeNone:

		// handle clicks locally

		return
	case terminal.MouseModeX10: //X10 compatibility mode

		/*
			X10 compatibility mode sends an escape sequence only on button press, encoding the location and the mouse button pressed.

			It is enabled by specifying parameter 9 to DECSET.

			On button press, xterm sends CSI M C b C x C y (6 characters).

			C b is button−1.

			C x and C y are the x and y coordinates of the mouse when the button was pressed.
		*/

		if action == glfw.Press {
			b := rune(button)
			packet := fmt.Sprintf("\x1b[M%c%c%c", (rune(b + 32)), (rune(tx + 32)), (rune(ty + 32)))

			gui.terminal.Write([]byte(packet))
		}
	case terminal.MouseModeVT200: // normal
		/*

			Normal tracking mode sends an escape sequence on both button press and release.

			Modifier key (shift, ctrl, meta) information is also sent.

			It is enabled by specifying parameter 1000 to DECSET.

			On button press or release, xterm sends CSI M C b C x C y .

			The low two bits of C b encode button information: 0=MB1 pressed, 1=MB2 pressed, 2=MB3 pressed, 3=release.

			The next three bits encode the modifiers which were down when the button was pressed and are added together: 4=Shift, 8=Meta, 16=Control.

			Note however that the shift and control bits are normally unavailable because xterm uses the control modifier with mouse for popup menus,
			and the shift modifier is used in the default translations for button events. The Meta modifier recognized by xterm is the mod1 mask, and
			is not necessarily the "Meta" key (see xmodmap).

			C x and C y are the x and y coordinates of the mouse event, encoded as in X10 mode.

			Wheel mice may return buttons 4 and 5. Those buttons are represented by the same event codes as buttons 1 and 2 respectively, except that 64 is added to the event code. Release events for the wheel buttons are not reported.
		*/
		gui.emitButtonEventToTerminal(tx, ty, button, &action, mod)

	case terminal.MouseModeVT200Highlight:
		/*
		   Mouse highlight tracking notifies a program of a button press, receives a range of lines from the program, highlights the region covered by the mouse within that range until button release, and then sends the program the release coordinates. It is enabled by specifying parameter 1001 to DECSET. Highlighting is performed only for button 1, though other button events can be received. Warning: use of this mode requires a cooperating program or it will hang xterm. On button press, the same information as for normal tracking is generated; xterm then waits for the program to send mouse tracking information. All X events are ignored until the proper escape sequence is received from the pty: CSI P s ; P s ; P s ; P s ; P s T . The parameters are func, startx, starty, firstrow, and lastrow. func is non-zero to initiate highlight tracking and zero to abort. startx and starty give the starting x and y location for the highlighted region. The ending location tracks the mouse, but will never be above row firstrow and will always be above row lastrow. (The top of the screen is row 1.) When the button is released, xterm reports the ending position one of two ways: if the start and end coordinates are valid text locations: CSI t C x C y . If either coordinate is past the end of the line: CSI T C x C y C x C y C x C y . The parameters are startx, starty, endx, endy, mousex, and mousey. startx, starty, endx, and endy give the starting and ending character positions of the region. mousex and mousey give the location of the mouse at button up, which may not be over a character.
		*/
		panic("VT200 mouse highlight mode not supported")

	case terminal.MouseModeButtonEvent:
		/*
		   Button-event tracking is essentially the same as normal tracking, but xterm also reports button-motion events.
		   Motion events are reported only if the mouse pointer has moved to a different character cell. It is enabled by specifying parameter 1002 to DECSET.
		   On button press or release, xterm sends the same codes used by normal tracking mode.
		   On button-motion events, xterm adds 32 to the event code (the third character, C b ).
		   The other bits of the event code specify button and modifier keys as in normal mode.
		   For example, motion into cell x,y with button 1 down is reported as CSI M @ C x C y . ( @ = 32 + 0 (button 1) + 32 (motion indicator) ).
		   Similarly, motion with button 3 down is reported as CSI M B C x C y . ( B = 32 + 2 (button 3) + 32 (motion indicator) ).
		*/
		gui.emitButtonEventToTerminal(tx, ty, button, &action, mod)

	case terminal.MouseModeAnyEvent:
		/*
		   Any-event mode is the same as button-event mode, except that all motion events are reported, even if no mouse button is down. It is enabled by specifying 1003 to DECSET.


		*/
		panic("Mouse any event mode not supported")

	default:
		panic("Unsupported mouse mode")
	}

}

func (gui *GUI) handleSelectionButtonPress(x uint16, y uint16, mod glfw.ModifierKey) {
	activeBuffer := gui.terminal.ActiveBuffer()
	clickCount := gui.updateLeftClickCount(x, y)
	gui.updateSelectionMode(mod)
	switch clickCount {
	case 1:
		activeBuffer.StartSelection(x, y, buffer.SelectionChar)
	case 2:
		activeBuffer.StartSelection(x, y, buffer.SelectionWord)
	case 3:
		activeBuffer.StartSelection(x, y, buffer.SelectionLine)
	}
	gui.mouseMovedAfterSelectionStarted = false
}

func (gui *GUI) handleSelectionButtonRelease(x uint16, y uint16) {
	activeBuffer := gui.terminal.ActiveBuffer()
	if x != gui.prevLeftClickX || y != gui.prevLeftClickY {
		gui.mouseMovedAfterSelectionStarted = true
	}

	if gui.leftClickCount != 1 || gui.mouseMovedAfterSelectionStarted {
		activeBuffer.ExtendSelection(x, y, true)
	}

	// Do copy to clipboard *or* open URL, but not both.
	handled := false
	if gui.config.CopyAndPasteWithMouse {
		selectedText := activeBuffer.GetSelectedText(gui.selectionRegionMode)
		if selectedText != "" {
			gui.window.SetClipboardString(selectedText)
			handled = true
		}
	}

	if !handled {
		if url := activeBuffer.GetURLAtPosition(x, y); url != "" {
			go gui.launchTarget(url)
		}
	}
}

func (gui *GUI) emitButtonEventToTerminal(tx int, ty int, button glfw.MouseButton, action *glfw.Action, mod glfw.ModifierKey) {
	motion := action == nil

	release := false
	if !motion {
		if *action == glfw.Release {
			release = true
		} else if *action != glfw.Press {
			return
		}
	}

	ext := gui.terminal.GetMouseExtMode()

	// For SGR, normal button encoding (as for Press event)
	b, ok := btnCode(button, release && ext != terminal.MouseExtSGR, mod)

	if !ok {
		return // unknown button
	}

	// @todo check limits for non-SGR encoding

	if motion {
		b |= 32

		// after applying limits we can check the final values
		if tx == gui.prevMotionTX && ty == gui.prevMotionTY {
			return
		}
	}

	gui.prevMotionTX = tx
	gui.prevMotionTY = ty

	var packet string
	switch ext {
	case terminal.MouseExtSGR:
		final := 'M'
		if release {
			final = 'm'
		}
		packet = fmt.Sprintf("\x1b[<%d;%d;%d%c", b, tx, ty, final)
	case terminal.MouseExtURXVT:
		packet = fmt.Sprintf("\x1b[%d;%d;%dM", b+32, tx, ty)
	default:
		packet = fmt.Sprintf("\x1b[M%c%c%c", (rune(b + 32)), (rune(tx + 32)), (rune(ty + 32)))
	}
	gui.logger.Infof("Sending mouse packet: '%v'", packet)
	gui.terminal.Write([]byte(packet))
}
