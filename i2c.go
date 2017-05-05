// i2c libary for intel edison
// Taken for the embed project changes made for intel edison

// The MIT License (MIT)
// Copyright (c) 2015 NeuralSpaz
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

package i2c

import (
	"fmt"
	"os"
	"reflect"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// S     (1 bit) : Start bit
// P     (1 bit) : Stop bit
// Rd/Wr (1 bit) : Read/Write bit. Rd equals 1, Wr equals 0.
// A, NA (1 bit) : Accept and reverse accept bit.
// Addr  (7 bits): I2C 7 bit address. Note that this can be expanded as usual to
//                 get a 10 bit I2C address.
// Comm  (8 bits): Command byte, a data byte which often selects a register on
//                 the device.
// Data  (8 bits): A plain data byte. Sometimes, I write DataLow, DataHigh
//                 for 16 bit data.
// Count (8 bits): A data byte containing the length of a block operation.
// {..}: Data sent by I2C slave, as opposed to data sent by master.
type I2CBus interface {
	// ReadByte reads a byte from the given address.
	// S Addr Rd {A} {value} NA P
	ReadByte(addr byte) (value byte, err error)
	// WriteByte writes a byte to the given address.
	// S Addr Wr {A} value {A} P
	WriteByte(addr, value byte) error
	// WriteBytes writes a slice bytes to the given address.
	// S Addr Wr {A} value[0] {A} value[1] {A} ... {A} value[n] {A} NA P
	WriteBytes(addr byte, value []byte) error
	// ReadFromReg reads n (len(value)) bytes from the given address and register.
	ReadFromReg(addr, reg byte, value []byte) error
	// ReadByteFromReg reads a byte from the given address and register.
	ReadByteFromReg(addr, reg byte) (value byte, err error)
	// ReadU16FromReg reads a unsigned 16 bit integer from the given address and register.
	ReadWordFromReg(addr, reg byte) (value uint16, err error)
	ReadWordFromRegLSBF(addr, reg byte) (value uint16, err error)
	// WriteToReg writes len(value) bytes to the given address and register.
	WriteToReg(addr, reg byte, value []byte) error
	// WriteByteToReg writes a byte to the given address and register.
	WriteByteToReg(addr, reg, value byte) error
	// WriteU16ToReg
	WriteWordToReg(addr, reg byte, value uint16) error
	// Close releases the resources associated with the bus.
	Close() error
}

const (
	delay    = 1      // delay in milliseconds
	slaveCmd = 0x0703 // Cmd to set slave address
	rdrwCmd  = 0x0707 // Cmd to read/write data together
	rd       = 0x0001
)

type i2c_msg struct {
	addr  uint16
	flags uint16
	len   uint16
	buf   uintptr
}

type i2c_rdwr_ioctl_data struct {
	msgs uintptr
	nmsg uint32
}

type i2cBus struct {
	l    byte
	file *os.File
	addr byte
	mu   sync.Mutex

	initialized bool
}

// Returns New i2c interfce on bus use i2cdetect to find out which bus you to use
func NewI2CBus(l byte) I2CBus {
	return &i2cBus{l: l}
}

func (b *i2cBus) init() error {
	if b.initialized {
		return nil
	}

	var err error
	if b.file, err = os.OpenFile(fmt.Sprintf("/dev/i2c-%v", b.l), os.O_RDWR, os.ModeExclusive); err != nil {
		return err
	}

	fmt.Println("i2c: bus %v initialized", b.l)

	b.initialized = true

	return nil
}

func (b *i2cBus) setAddress(addr byte) error {
	if addr != b.addr {
		fmt.Println("i2c: setting bus %v address to %#02x", b.l, addr)
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, b.file.Fd(), slaveCmd, uintptr(addr)); errno != 0 {
			return syscall.Errno(errno)
		}

		b.addr = addr
	}

	return nil
}

// ReadByte reads a byte from the given address.
func (b *i2cBus) ReadByte(addr byte) (byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.init(); err != nil {
		return 0, err
	}

	if err := b.setAddress(addr); err != nil {
		return 0, err
	}

	bytes := make([]byte, 1)
	n, _ := b.file.Read(bytes)

	if n != 1 {
		return 0, fmt.Errorf("i2c: Unexpected number (%v) of bytes read", n)
	}

	return bytes[0], nil
}

// WriteByte writes a byte to the given address.
func (b *i2cBus) WriteByte(addr, value byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.init(); err != nil {
		return err
	}

	if err := b.setAddress(addr); err != nil {
		return err
	}

	n, err := b.file.Write([]byte{value})

	if n != 1 {
		err = fmt.Errorf("i2c: Unexpected number (%v) of bytes written in WriteByte", n)
	}

	return err
}

// WriteBytes writes a slice bytes to the given address.
func (b *i2cBus) WriteBytes(addr byte, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.init(); err != nil {
		return err
	}

	if err := b.setAddress(addr); err != nil {
		return err
	}

	for i := range value {
		n, err := b.file.Write([]byte{value[i]})

		if n != 1 {
			return fmt.Errorf("i2c: Unexpected number (%v) of bytes written in WriteBytes", n)
		}
		if err != nil {
			return err
		}

		time.Sleep(delay * time.Millisecond)
	}

	return nil
}

// ReadFromReg reads n (len(value)) bytes from the given address and register.
func (b *i2cBus) ReadFromReg(addr, reg byte, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.init(); err != nil {
		return err
	}

	if err := b.setAddress(addr); err != nil {
		return err
	}

	hdrp := (*reflect.SliceHeader)(unsafe.Pointer(&value))

	var messages [2]i2c_msg
	messages[0].addr = uint16(addr)
	messages[0].flags = 0
	messages[0].len = 1
	messages[0].buf = uintptr(unsafe.Pointer(&reg))

	messages[1].addr = uint16(addr)
	messages[1].flags = rd
	messages[1].len = uint16(len(value))
	messages[1].buf = uintptr(unsafe.Pointer(hdrp.Data))

	var packets i2c_rdwr_ioctl_data

	packets.msgs = uintptr(unsafe.Pointer(&messages))
	packets.nmsg = 2

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, b.file.Fd(), rdrwCmd, uintptr(unsafe.Pointer(&packets))); errno != 0 {
		return syscall.Errno(errno)
	}

	return nil
}

// ReadByteFromReg reads a byte from the given address and register.
func (b *i2cBus) ReadByteFromReg(addr, reg byte) (byte, error) {
	buf := make([]byte, 1)
	if err := b.ReadFromReg(addr, reg, buf); err != nil {
		return 0, err
	}
	return buf[0], nil
}

// Read single word from register first byte is MSB
func (b *i2cBus) ReadWordFromReg(addr, reg byte) (uint16, error) {
	buf := make([]byte, 2)
	if err := b.ReadFromReg(addr, reg, buf); err != nil {
		return 0, err
	}
	return uint16((uint16(buf[0]) << 8) | uint16(buf[1])), nil
}

// Read single word from register first byte is LSB
func (b *i2cBus) ReadWordFromRegLSBF(addr, reg byte) (uint16, error) {
	buf := make([]byte, 2)
	if err := b.ReadFromReg(addr, reg, buf); err != nil {
		return 0, err
	}
	return uint16((uint16(buf[1]) << 8) | uint16(buf[0])), nil
}

//Write []byte word to resgister
func (b *i2cBus) WriteToReg(addr, reg byte, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.init(); err != nil {
		return err
	}

	if err := b.setAddress(addr); err != nil {
		return err
	}

	outbuf := append([]byte{reg}, value...)

	hdrp := (*reflect.SliceHeader)(unsafe.Pointer(&outbuf))

	var message i2c_msg
	message.addr = uint16(addr)
	message.flags = 0
	message.len = uint16(len(outbuf))
	message.buf = uintptr(unsafe.Pointer(&hdrp.Data))

	var packets i2c_rdwr_ioctl_data

	packets.msgs = uintptr(unsafe.Pointer(&message))
	packets.nmsg = 1

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, b.file.Fd(), rdrwCmd, uintptr(unsafe.Pointer(&packets))); errno != 0 {
		return syscall.Errno(errno)
	}

	return nil
}

// Write single Byte to register
func (b *i2cBus) WriteByteToReg(addr, reg, value byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.init(); err != nil {
		return err
	}

	if err := b.setAddress(addr); err != nil {
		return err
	}

	outbuf := [...]byte{
		reg,
		value,
	}

	var message i2c_msg
	message.addr = uint16(addr)
	message.flags = 0
	message.len = uint16(len(outbuf))
	message.buf = uintptr(unsafe.Pointer(&outbuf))

	var packets i2c_rdwr_ioctl_data

	packets.msgs = uintptr(unsafe.Pointer(&message))
	packets.nmsg = 1

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, b.file.Fd(), rdrwCmd, uintptr(unsafe.Pointer(&packets))); errno != 0 {
		return syscall.Errno(errno)
	}

	return nil
}

// Write Single Word to Register
func (b *i2cBus) WriteWordToReg(addr, reg byte, value uint16) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.init(); err != nil {
		return err
	}

	if err := b.setAddress(addr); err != nil {
		return err
	}

	outbuf := [...]byte{
		reg,
		byte(value >> 8),
		byte(value),
	}

	var messages i2c_msg
	messages.addr = uint16(addr)
	messages.flags = 0
	messages.len = uint16(len(outbuf))
	messages.buf = uintptr(unsafe.Pointer(&outbuf))

	var packets i2c_rdwr_ioctl_data

	packets.msgs = uintptr(unsafe.Pointer(&messages))
	packets.nmsg = 1

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, b.file.Fd(), rdrwCmd, uintptr(unsafe.Pointer(&packets))); errno != 0 {
		return syscall.Errno(errno)
	}

	return nil
}

// Close i2c file
func (b *i2cBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.initialized {
		return nil
	}

	return b.file.Close()
}
