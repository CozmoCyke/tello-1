// autopilot.go

// This file contains Tello flight command API except for stick control.

// Copyright (C) 2018  Steve Merrony

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package tello

import (
	"errors"
	"log"
	"time"
)

const (
	autopilotPeriodMs = 25 // how often the autopilot(s) monitor the drone
	// AutoHeightLimitDm is the maximum vertical displacement allowed for AutoFlyToHeight() etc. in decimetres.
	AutoHeightLimitDm = 300
)

// CancelAutoFlyToHeight stops any in-flight AutoFlyToHeight navigation.
// The drone should stop moving vertically.
func (tello *Tello) CancelAutoFlyToHeight() {
	tello.autoHeightMu.Lock()
	tello.autoHeight = false
	tello.autoHeightMu.Unlock()
}

// AutoFlyToHeight starts vertical movement to the specified height in decimetres
// (so a value of 10 means 1m).
// The func returns immediately and a Goroutine handles the navigation until either
// it is complete or cancelled via CancelAutoFlyToHeight().
// The caller may optionally listen on the 'done' channel for a signal that
// the navigation is complete (or has been cancelled).
func (tello *Tello) AutoFlyToHeight(dm int16) (done chan bool, err error) {
	//log.Printf("AutoFlyToHeight called with height: %d\n", dm)
	if dm > AutoHeightLimitDm || dm < -AutoHeightLimitDm {
		return nil, errors.New("Verical navigation limit exceeded")
	}
	// are we already navigating?
	tello.autoHeightMu.RLock()
	if tello.autoHeight {
		tello.autoHeightMu.RUnlock()
		return nil, errors.New("Already navigating vertically")
	}
	tello.autoHeightMu.RUnlock()

	tello.autoHeightMu.Lock()
	tello.autoHeight = true
	tello.autoHeightMu.Unlock()

	done = make(chan bool, 1) // buffered so send doesn't block

	//log.Println("Autoheight set - starting goroutine")

	go func() {
		for {
			// has autoflight been cancelled?
			tello.autoHeightMu.RLock()
			cancelled := tello.autoHeight == false
			tello.autoHeightMu.RUnlock()
			if cancelled {
				log.Println("Cancelled")
				// stop vertical movement
				tello.ctrlMu.Lock()
				tello.ctrlLy = 0
				tello.ctrlMu.Unlock()
				tello.sendStickUpdate()
				done <- true
				return
			}

			tello.fdMu.RLock()
			delta := dm - tello.fd.Height // delta will be positive if we are too low
			//log.Printf("Target: %d, Height: %d, Delta: %d\n", dm, tello.fd.Height, delta)
			tello.fdMu.RUnlock()

			tello.ctrlMu.Lock()
			switch {
			case delta > 4:
				tello.ctrlLy = 32500 // full throttle if >40cm off target
			case delta > 0:
				tello.ctrlLy = 16250 // half throttle if <40cm off target
			case delta < -4:
				tello.ctrlLy = -32500
			case delta < 0:
				tello.ctrlLy = -16250
			case delta == 0: // might need some 'tolerance' here?
				// we're there! Cancel...
				tello.autoHeightMu.Lock()
				tello.autoHeight = false
				tello.autoHeightMu.Unlock()
			}
			tello.ctrlMu.Unlock()
			tello.sendStickUpdate()

			time.Sleep(autopilotPeriodMs * time.Millisecond)
		}
	}()

	return done, nil
}

// CancelAutoTurn stops any in-flight AutoTurnToYaw or AutoTurnByDeg navigation.
// The drone should stop rotating.
func (tello *Tello) CancelAutoTurn() {
	tello.autoYawMu.Lock()
	tello.autoYaw = false
	tello.autoYawMu.Unlock()
}

// AutoTurnToYaw starts rotational movement to the specified yaw in degrees.
// The yaw should be between -180 and +180 degrees.
// The func returns immediately and a Goroutine handles the navigation.
// The caller may optionally listen on the 'done' channel for a signal that
// the navigation is complete (may have been cancelled).
// You may explicitly cancel this operation via CancelAutoTurn().
func (tello *Tello) AutoTurnToYaw(targetYaw int16) (done chan bool, err error) {
	//log.Printf("AutoTurnToYaw called with target: %d\n", targetYaw)
	if targetYaw < -180 || targetYaw > 180 {
		return nil, errors.New("Target yaw must be between -180 and +180")
	}
	adjustedTarget := targetYaw
	if targetYaw < 0 {
		adjustedTarget = 360 + targetYaw
	}

	// are we already navigating?
	tello.autoYawMu.RLock()
	if tello.autoYaw {
		tello.autoYawMu.RUnlock()
		return nil, errors.New("Already navigating rotationally")
	}
	tello.autoYawMu.RUnlock()

	tello.autoYawMu.Lock()
	tello.autoYaw = true
	tello.autoYawMu.Unlock()

	done = make(chan bool, 1) // buffered so send doesn't block

	//log.Println("autoYaw set - starting goroutine")

	go func() {
		for {
			// has autoflight been cancelled?
			tello.autoYawMu.RLock()
			cancelled := tello.autoYaw == false
			tello.autoYawMu.RUnlock()
			if cancelled {
				log.Println("Cancelled")
				// stop rotational movement
				tello.ctrlMu.Lock()
				tello.ctrlLx = 0
				tello.ctrlMu.Unlock()
				tello.sendStickUpdate()
				done <- true
				return
			}

			tello.fdMu.RLock()
			adjustedCurrent := tello.fd.IMU.Yaw
			tello.fdMu.RUnlock()
			if adjustedCurrent < 0 {
				adjustedCurrent = 360 + adjustedCurrent
			}

			delta := adjustedTarget - adjustedCurrent
			absDelta := int16Abs(delta)
			switch {
			case absDelta <= 180: //
			case delta > 0:
				delta = absDelta - 360
			case delta > 0:
				delta = 360 - absDelta
			}

			//log.Printf("Target: %d, Current: %d, Delta: %d\n", adjustedTarget, adjustedCurrent, delta)

			tello.ctrlMu.Lock()
			switch {
			case delta > 10:
				tello.ctrlLx = 32500 // full throttle if >10deg off target
			case delta > 0:
				tello.ctrlLx = 16250 // half throttle if <10deg off target
			case delta < -10:
				tello.ctrlLx = -32500
			case delta < 0:
				tello.ctrlLx = -16250
			case delta == 0: // might need some 'tolerance' here?
				// we're there! Cancel...
				tello.autoYawMu.Lock()
				tello.autoYaw = false
				tello.autoYawMu.Unlock()
			}
			tello.ctrlMu.Unlock()
			tello.sendStickUpdate()

			time.Sleep(autopilotPeriodMs * time.Millisecond)
		}
	}()

	return done, nil
}

// AutoTurnByDeg starts rotational movement by the specified amount degrees.
// The amount should be between -180 and +180 degrees, where negative values cause an
// anticlockwise rotation and vice-versa.
// The func returns immediately and a Goroutine handles the navigation.
// The caller may optionally listen on the 'done' channel for a signal that
// the navigation is complete (may have been cancelled).
// You may explicitly cancel this operation via CancelAutoTurn().
func (tello *Tello) AutoTurnByDeg(delta int16) (done chan bool, err error) {

	if delta < -180 || delta > 180 {
		return nil, errors.New("Turn amount must be between -180 and +180")
	}

	// are we already navigating?
	tello.autoYawMu.RLock()
	if tello.autoYaw {
		tello.autoYawMu.RUnlock()
		return nil, errors.New("Already navigating rotationally")
	}
	tello.autoYawMu.RUnlock()

	tello.fdMu.RLock()
	adjustedTarget := tello.fd.IMU.Yaw
	tello.fdMu.RUnlock()

	adjustedTarget += delta
	switch {
	case adjustedTarget == 360:
		adjustedTarget = 0
	case adjustedTarget > 180:
		adjustedTarget = -(360 - adjustedTarget)
	case adjustedTarget < -180:
		adjustedTarget = 360 + adjustedTarget
	}

	return tello.AutoTurnToYaw(adjustedTarget)
}

func int16Abs(x int16) int16 {
	if x < 0 {
		return -x
	}
	return x
}
