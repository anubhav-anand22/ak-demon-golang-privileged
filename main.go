package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"os/exec"

	// "strings"
	"syscall"
	"time"

	"tinygo.org/x/bluetooth"
)

const (
	socketPath       = "/var/run/ble_bridge.sock"
	TargetDeviceName = "realme C67 5G"
)

var (
	TargetServiceUUID, _ = bluetooth.ParseUUID("0000180D-0000-1000-8000-00805F9B34FB")
	TargetCharUUID, _    = bluetooth.ParseUUID("00002A37-0000-1000-8000-00805F9B34FB")
	adapter              = bluetooth.DefaultAdapter
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.SetPrefix("[PRIVILEGED] ")

	if os.Geteuid() != 0 {
		log.Fatal("This process must be run as root/admin.")
	}

	for {
		setupBluetooth()

		// adapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
		// 	if connected {
		// 		println("device connected:", device.Address.String())
		// 		return
		// 	}

		// 	println("device disconnected:", device.Address.String())
		// 	cancel()
		// })

		device, targetChar, err := scanAndConnect(adapter)
		if err != nil {
			log.Printf("Scan failed: %v. Retrying...", err)
			time.Sleep(2 * time.Second)
			continue
		}

		log.Printf("Connected to %s. Starting unprivileged worker...", TargetDeviceName)

		runWorkerLoop(device, targetChar)

		log.Println("Worker or Connection lost. Restarting cycle...")
		time.Sleep(2 * time.Second)
	}
}

func setupBluetooth() {
	exec.Command("rfkill", "unblock", "bluetooth").Run()
	if err := adapter.Enable(); err != nil {
		log.Fatalf("Failed to enable Bluetooth adapter: %v", err)
	}
}

func scanAndConnect(a *bluetooth.Adapter) (device *bluetooth.Device, targetChar *bluetooth.DeviceCharacteristic, e error) {
	// found := make(chan bluetooth.ScanResult)

	log.Printf("Scanning for Device: '%s' or UUID: %s", TargetDeviceName, TargetServiceUUID.String())

	err := a.Scan(func(ad *bluetooth.Adapter, result bluetooth.ScanResult) {
		services := result.ServiceUUIDs()
		serviceData := result.ServiceData()

		for _, service := range services {
			log.Printf("name: %s, mac: %s", service, result.Address.String())
			log.Printf("isMatch: %t", service.String() == TargetServiceUUID.String())
			log.Printf("serviceData: %s", serviceData)
			if service.String() == TargetServiceUUID.String() {
				log.Printf("Match found: UUID(%s)", service)

				d, err := ad.Connect(result.Address, bluetooth.ConnectionParams{})
				if err != nil {
					log.Printf("Error (a.connect error)")

				}
				srvcs, err := d.DiscoverServices([]bluetooth.UUID{TargetServiceUUID})
				if err != nil || len(srvcs) == 0 {
					log.Printf("Failed to find service")
					return
				}
				chars, err := srvcs[0].DiscoverCharacteristics([]bluetooth.UUID{TargetCharUUID})
				if err != nil || len(chars) == 0 {
					log.Printf("Failed to find characteristic")
					return
				}
				device = &d
				log.Printf("BT connection success")
				targetChar = &chars[0]
				log.Printf("Characteristic found! Ready to send data.")
				ad.StopScan()

				// found <- result
				return
			}
		}

	})

	if err != nil {
		log.Printf("Error (a.scan error)")
		e = err
		return
	}
	e = nil
	return

}

func runWorkerLoop(device *bluetooth.Device, targetChar *bluetooth.DeviceCharacteristic) {
	_ = os.Remove(socketPath)
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		log.Printf("Socket error: %v", err)
		return
	}
	defer l.Close()

	os.Chmod(socketPath, 0666)

	cmd := exec.Command("/usr/local/bin/unprivileged")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 1000, Gid: 1000},
	}
	cmd.Env = append(os.Environ(),
		"HOME=/home/anubhav_anand",
		"USER=anubhav_anand",
	)

	if err := cmd.Start(); err != nil {
		log.Printf("Worker start error: %v", err)
		return
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	go func() {
		for {
			conn, err := l.AcceptUnix()
			if err != nil {
				return
			}

			rawConn, _ := conn.File()
			ucred, err := syscall.GetsockoptUcred(int(rawConn.Fd()), syscall.SOL_SOCKET, syscall.SO_PEERCRED)

			if err == nil && int(ucred.Pid) == cmd.Process.Pid {
				go handleIPC(conn, device, targetChar)
			} else {
				conn.Close()
			}
		}
	}()

	<-done
	device.Disconnect()
}

func handleIPC(conn *net.UnixConn, device *bluetooth.Device, targetChar *bluetooth.DeviceCharacteristic) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var req map[string]string
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			log.Printf("Error %s", err)
			continue
		}

		rawMsg := scanner.Bytes()

		msg, err, didDefaulted := ParseJsonMsgFromUnPri(rawMsg)

		if err != nil {
			log.Fatal(err)
			continue
		}

		if didDefaulted {
			log.Printf("Msg defaulted:-")
			log.Printf("%s", scanner.Text())
			continue
		}

		switch v := msg.(type) {
		case TestMstToPriTypeMsg:
			log.Printf("Logged from unprivileged app")
		case TestMstToMobBtTypeMsg:
			log.Printf("Logged from unprivileged app to mob via bt")
			log.Printf("Sending JSON to mobile via BT")

			// 1. Convert the struct back to a JSON byte array
			jsonData, _ := json.Marshal(v)

			// 2. Add an EOF marker so Android knows when to parse it
			jsonData = append(jsonData, '\n')

			// 3. Chunk and send!
			for i := 0; i < len(jsonData); i += 20 {
				end := i + 20
				if end > len(jsonData) {
					end = len(jsonData)
				}

				// Write Without Response is much faster for chunked data
				_, writeErr := targetChar.WriteWithoutResponse(jsonData[i:end])
				if writeErr != nil {
					log.Printf("Failed to write BT chunk: %v", writeErr)
					break
				}

				// Tiny delay to prevent overflowing the Bluetooth hardware buffer
				time.Sleep(10 * time.Millisecond)
			}
			log.Printf("Successfully sent %d bytes over BLE", len(jsonData))
		}

		// log.Printf("Msg: %s", scanner.Text())
		// log.Printf("req: %s", req["command"])

		// switch req["command"] {
		// case "PING":
		// 	conn.Write([]byte(`{"response": "PONG", "device": "connected"}` + "\n"))
		// case "GET_RSSI":

		// 	conn.Write([]byte(`{"rssi": "stable"}` + "\n"))
		// }
	}
}

// package main

// import (
// 	"bufio"
// 	"encoding/json"
// 	"log"
// 	"net"
// 	"os"
// 	"os/exec"
// 	"strings"
// 	"syscall"
// 	"time"

// 	"tinygo.org/x/bluetooth"
// )

// const (
// 	socketPath       = "/var/run/ble_bridge.sock"
// 	TargetDeviceName = "Go-Bridge-Key"
// )

// var TargetServiceUUID, _ = bluetooth.ParseUUID("550e8400-e29b-41d4-a716-446655440000")
// var adapter = bluetooth.DefaultAdapter

// func main() {
// 	if os.Geteuid() != 0 {
// 		log.Fatal("This process must be run as root/admin.")
// 	}

// 	for {
// 		setupBluetooth()

// 		// FIX: Pass the address of the adapter (&adapter)
// 		device, err := scanAndConnect(adapter)
// 		if err != nil {
// 			log.Printf("Scan failed: %v. Retrying...", err)
// 			time.Sleep(2 * time.Second)
// 			continue
// 		}

// 		log.Printf("Connected to %s. Starting unprivileged worker...", TargetDeviceName)
// 		runWorkerLoop(*device)

// 		log.Println("Worker or Connection lost. Restarting cycle...")
// 		time.Sleep(2 * time.Second)
// 	}
// }

// func setupBluetooth() {
// 	exec.Command("rfkill", "unblock", "bluetooth").Run()
// 	if err := adapter.Enable(); err != nil {
// 		log.Fatalf("Failed to enable Bluetooth adapter: %v", err)
// 	}
// }

// // FIX: Ensure return type is (bluetooth.Device, error)
// func scanAndConnect(a *bluetooth.Adapter) (*bluetooth.Device, error) {
// 	found := make(chan bluetooth.ScanResult)

// 	log.Printf("Scanning for Device: '%s' with UUID: %s", TargetDeviceName, TargetServiceUUID.String())

// 	err := a.Scan(func(ad *bluetooth.Adapter, result bluetooth.ScanResult) {
// 		uuidFound := false
// 		// FIX: Correct way to access Service UUIDs in current tinygo/bluetooth
// 		for _, service := range result.ServiceUUIDs() {
// 			if service == TargetServiceUUID {
// 				uuidFound = true
// 				break
// 			}
// 		}

// 		if uuidFound {
// 			currentName := result.LocalName()
// 			if strings.TrimSpace(currentName) == TargetDeviceName {
// 				log.Printf("Match Found! Name: %s, Address: %s", currentName, result.Address.String())
// 				ad.StopScan()
// 				found <- result
// 			}
// 		}
// 	})

// 	if err != nil {
// 		// This nil is now valid because the return type is an interface
// 		return nil, err
// 	}

// 	result := <-found

// 	// Connect returns (Device, error)
// 	device, err := a.Connect(result.Address, bluetooth.ConnectionParams{})
// 	if err != nil {
// 		return nil, err
// 	}

// 	return &device, nil
// }

// func runWorkerLoop(device bluetooth.Device) {
// 	_ = os.Remove(socketPath)
// 	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
// 	if err != nil {
// 		log.Printf("Socket error: %v", err)
// 		return
// 	}
// 	defer l.Close()
// 	os.Chmod(socketPath, 0660)

// 	cmd := exec.Command("/usr/local/bin/unprivileged")
// 	cmd.SysProcAttr = &syscall.SysProcAttr{
// 		Credential: &syscall.Credential{Uid: 1000, Gid: 1000},
// 	}

// 	if err := cmd.Start(); err != nil {
// 		log.Printf("Worker start error: %v", err)
// 		return
// 	}

// 	done := make(chan error, 1)
// 	go func() { done <- cmd.Wait() }()

// 	go func() {
// 		for {
// 			conn, err := l.AcceptUnix()
// 			if err != nil {
// 				return
// 			}

// 			rawConn, _ := conn.File()
// 			ucred, err := syscall.GetsockoptUcred(int(rawConn.Fd()), syscall.SOL_SOCKET, syscall.SO_PEERCRED)

// 			if err == nil && int(ucred.Pid) == cmd.Process.Pid {
// 				go handleIPC(conn, device)
// 			} else {
// 				conn.Close()
// 			}
// 		}
// 	}()

// 	<-done
// 	device.Disconnect()
// }

// func handleIPC(conn *net.UnixConn, device bluetooth.Device) {
// 	defer conn.Close()
// 	scanner := bufio.NewScanner(conn)
// 	for scanner.Scan() {
// 		var req map[string]string
// 		json.Unmarshal(scanner.Bytes(), &req)

// 		switch req["command"] {
// 		case "PING":
// 			conn.Write([]byte(`{"response": "PONG", "device": "connected"}` + "\n"))
// 		case "GET_RSSI":
// 			conn.Write([]byte(`{"rssi": "stable"}` + "\n"))
// 		}
// 	}
// }

// package main

// import (
// 	"bufio"
// 	"encoding/json"
// 	"log"
// 	"net"
// 	"os"
// 	"os/exec"
// 	"syscall"
// 	"time"
// 	"strings"

// 	"tinygo.org/x/bluetooth"
// )

// const (
// 	socketPath       = "/var/run/ble_bridge.sock"
// 	TargetDeviceName = "Go-Bridge-Key"
// )

// var TargetServiceUUID, _ = bluetooth.ParseUUID("550e8400-e29b-41d4-a716-446655440000")

// var adapter = bluetooth.DefaultAdapter

// func main() {
// 	if os.Geteuid() != 0 {
// 		log.Fatal("This process must be run as root/admin.")
// 	}

// 	for {
// 		setupBluetooth()
// 		device, _ := scanAndConnect(adapter)

// 		log.Printf("Connected to %s. Starting unprivileged worker...", TargetDeviceName)
// 		runWorkerLoop(device)

// 		log.Println("Worker or Connection lost. Restarting cycle...")
// 		time.Sleep(2 * time.Second)
// 	}
// }

// func setupBluetooth() {
// 	// Force enable Bluetooth via rfkill and adapter Enable
// 	exec.Command("rfkill", "unblock", "bluetooth").Run()
// 	if err := adapter.Enable(); err != nil {
// 		log.Fatalf("Failed to enable Bluetooth adapter: %v", err)
// 	}
// }

// func scanAndConnect(adapter *bluetooth.Adapter) (bluetooth.Device, error) {
// 	found := make(chan bluetooth.ScanResult)

// 	log.Printf("Scanning for Device: '%s' with UUID: %s", TargetDeviceName, TargetServiceUUID.String())

// 	err := adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
// 		uuidFound := false
// 		for _, service := range result.AdvertisementPayload.ServiceUUIDs() {
// 			if service == TargetServiceUUID {
// 				uuidFound = true
// 				break
// 			}
// 		}

// 		if uuidFound {
// 			currentName := result.LocalName()
// 			if strings.TrimSpace(currentName) == TargetDeviceName {
// 				log.Printf("Match Found! Name: %s, Address: %s", currentName, result.Address.String())
// 				adapter.StopScan()
// 				found <- result
// 			}
// 		}
// 	})

// 	if err != nil {
// 		return nil, err
// 	}

// 	result := <-found

// 	device, err := adapter.Connect(result.Address, bluetooth.ConnectionParams{})
// 	if err != nil {
// 		return nil, err
// 	}

// 	return device, nil
// }

// // func scanAndConnect() bluetooth.Device {
// // 	// var target bluetooth.Device
// // 	found := make(chan bluetooth.ScanResult)

// // 	go func() {
// // 		adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
// // 			if result.Address.String() == targetMAC {
// // 				adapter.StopScan()
// // 				found <- result
// // 			}
// // 		})
// // 	}()

// // 	result := <-found
// // 	device, err := adapter.Connect(result.Address, bluetooth.ConnectionParams{})
// // 	if err != nil {
// // 		log.Fatalf("Failed to connect: %v", err)
// // 	}
// // 	return device
// // }

// func runWorkerLoop(device bluetooth.Device) {
// 	_ = os.Remove(socketPath)
// 	l, _ := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
// 	defer l.Close()
// 	os.Chmod(socketPath, 0660)

// 	// Start worker as unprivileged user (UID 1000)
// 	// cmd := exec.Command("./unprivileged")
// 	cmd := exec.Command("/usr/local/bin/unprivileged")
// 	cmd.SysProcAttr = &syscall.SysProcAttr{
// 		Credential: &syscall.Credential{Uid: 1000, Gid: 1000},
// 	}

// 	if err := cmd.Start(); err != nil {
// 		log.Printf("Worker start error: %v", err)
// 		return
// 	}

// 	// Channel to monitor process exit
// 	done := make(chan error, 1)
// 	go func() { done <- cmd.Wait() }()

// 	go func() {
// 		for {
// 			conn, err := l.AcceptUnix()
// 			if err != nil {
// 				return
// 			}

// 			// SECURITY: Verify caller is the actual child PID
// 			rawConn, _ := conn.File()
// 			ucred, _ := syscall.GetsockoptUcred(int(rawConn.Fd()), syscall.SOL_SOCKET, syscall.SO_PEERCRED)

// 			if int(ucred.Pid) == cmd.Process.Pid {
// 				go handleIPC(conn, device)
// 			} else {
// 				conn.Close()
// 			}
// 		}
// 	}()

// 	<-done // Wait until unprivileged app exits or is killed
// 	device.Disconnect()
// }

// func handleIPC(conn *net.UnixConn, device bluetooth.Device) {
// 	defer conn.Close()
// 	scanner := bufio.NewScanner(conn)
// 	for scanner.Scan() {
// 		var req map[string]string
// 		json.Unmarshal(scanner.Bytes(), &req)

// 		switch req["command"] {
// 		case "PING":
// 			conn.Write([]byte(`{"response": "PONG", "device": "connected"}` + "\n"))
// 		case "GET_RSSI":
// 			// Example of a privileged hardware task
// 			conn.Write([]byte(`{"rssi": "stable"}` + "\n"))
// 		}
// 	}
// }
