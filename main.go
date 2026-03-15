package main

import (
	// "bufio"
	// "encoding/json"
	"log"
	// "net"
	"os"
	"os/exec"

	// "strings"
	// "context"
	// "syscall"
	// "time"
	// "fmt"

	"tinygo.org/x/bluetooth"
)

const (
	socketPath       = "/var/run/ble_bridge.sock"
	TargetDeviceName = "realme C67 5G"
)

var (
	UUID, _     = bluetooth.ParseUUID("7a8e9c3b-5e2f-4d9b-b6f1-3c4a8d2e7f10")
	CharUUID, _ = bluetooth.ParseUUID("c1f4a2b8-6d7e-4a53-9f12-0e3b7c9d5a44")
	adapter     = bluetooth.DefaultAdapter
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.SetPrefix("[PRIVILEGED] ")

	if os.Geteuid() != 0 {
		log.Fatal("This process must be run as root/admin.")
	}

	for {
		setupBluetooth()
		setupPref()

		// adapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
		// 	if connected {
		// 		println("device connected:", device.Address.String())
		// 		return
		// 	}

		// 	println("device disconnected:", device.Address.String())
		// 	cancel()
		// })

		// device, targetChar, err := scanAndConnect(adapter)
		// if err != nil {
		// 	log.Printf("Scan failed: %v. Retrying...", err)
		// 	time.Sleep(2 * time.Second)
		// 	continue
		// }

		// log.Printf("Connected to %s. Starting unprivileged worker...", TargetDeviceName)

		// runWorkerLoop(device, targetChar)

		// log.Println("Worker or Connection lost. Restarting cycle...")
		// time.Sleep(2 * time.Second)
	}
}

func setupPref() {
	log.Println("Enabling Bluetooth adapter...")
	err := adapter.Enable()
	if err != nil {
		log.Fatalf("Failed to enable BLE adapter: %v", err)
	}

	// 3. Configure the Characteristic
	// This dictates what the connecting phone is allowed to do, and how Go reacts.
	charConfig := bluetooth.CharacteristicConfig{
		UUID: CharUUID,
		// We want the phone to be able to Read and Write to this characteristic
		Flags: bluetooth.CharacteristicReadPermission | bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicWriteWithoutResponsePermission,

		// Initial value if the phone tries to read it
		Value: []byte("HELLO_FROM_GO"),

		// This callback fires whenever the React Native app writes data to us
		WriteEvent: func(client bluetooth.Connection, offset int, value []byte) {
			log.Printf("Received data from mobile: %s", string(value))

			// If you are sending chunks, you would append them to a buffer here
			// just like we did on the React Native side previously!
		},
	}

	// 4. Bundle the Characteristic into a Service
	service := bluetooth.Service{
		UUID:            UUID,
		Characteristics: []bluetooth.CharacteristicConfig{charConfig},
	}

	// 5. Add the Service to the Bluetooth Adapter
	err = adapter.AddService(&service)
	if err != nil {
		log.Fatalf("Failed to add service: %v", err)
	}

	// 6. Configure the Advertisement
	// This is the "beacon" signal the phone sees when scanning
	adv := adapter.DefaultAdvertisement()
	err = adv.Configure(bluetooth.AdvertisementOptions{
		LocalName:    "Go Security Key", // The name that will show up on the phone
		ServiceUUIDs: []bluetooth.UUID{UUID},
	})
	if err != nil {
		log.Fatalf("Failed to configure advertisement: %v", err)
	}

	// 7. Start Broadcasting!
	err = adv.Start()
	if err != nil {
		log.Fatalf("Failed to start advertising: %v", err)
	}

	log.Println("Peripheral is now Advertising! Waiting for connections...")

	// 8. Keep the program running forever
	select {}
}

func setupBluetooth() {
	exec.Command("rfkill", "unblock", "bluetooth").Run()
	if err := adapter.Enable(); err != nil {
		log.Fatalf("Failed to enable Bluetooth adapter: %v", err)
	}
}

// func scanAndConnect(a *bluetooth.Adapter) (device *bluetooth.Device, targetChar *bluetooth.DeviceCharacteristic, e error) {
// 	// found := make(chan bluetooth.ScanResult)

// 	log.Printf("Scanning for Device: '%s' or UUID: %s", TargetDeviceName, TargetServiceUUID.String())

// 	err := a.Scan(func(ad *bluetooth.Adapter, result bluetooth.ScanResult) {
// 		services := result.ServiceUUIDs()
// 		serviceData := result.ServiceData()

// 		for _, service := range services {
// 			log.Printf("name: %s, mac: %s", service, result.Address.String())
// 			log.Printf("isMatch: %t", service.String() == TargetServiceUUID.String())
// 			log.Printf("serviceData: %s", serviceData)
// 			if service.String() == TargetServiceUUID.String() {
// 				log.Printf("Match found: UUID(%s)", service)

// 				d, err := ad.Connect(result.Address, bluetooth.ConnectionParams{})
// 				if err != nil {
// 					log.Printf("Error (a.connect error)")

// 				}
// 				srvcs, err := d.DiscoverServices([]bluetooth.UUID{TargetServiceUUID})
// 				if err != nil || len(srvcs) == 0 {
// 					log.Printf("Failed to find service")
// 					return
// 				}
// 				chars, err := srvcs[0].DiscoverCharacteristics([]bluetooth.UUID{TargetCharUUID})
// 				if err != nil || len(chars) == 0 {
// 					log.Printf("Failed to find characteristic")
// 					return
// 				}
// 				device = &d
// 				log.Printf("BT connection success")
// 				targetChar = &chars[0]
// 				log.Printf("Characteristic found! Ready to send data.")
// 				ad.StopScan()

// 				// found <- result
// 				return
// 			}
// 		}

// 	})

// 	if err != nil {
// 		log.Printf("Error (a.scan error)")
// 		e = err
// 		return
// 	}
// 	e = nil
// 	return

// }

// func runWorkerLoop(device *bluetooth.Device, targetChar *bluetooth.DeviceCharacteristic) {
// 	_ = os.Remove(socketPath)
// 	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
// 	if err != nil {
// 		log.Printf("Socket error: %v", err)
// 		return
// 	}
// 	defer l.Close()

// 	os.Chmod(socketPath, 0666)

// 	cmd := exec.Command("/usr/local/bin/unprivileged")
// 	cmd.SysProcAttr = &syscall.SysProcAttr{
// 		Credential: &syscall.Credential{Uid: 1000, Gid: 1000},
// 	}
// 	cmd.Env = append(os.Environ(),
// 		"HOME=/home/anubhav_anand",
// 		"USER=anubhav_anand",
// 	)

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
// 				go handleIPC(conn, device, targetChar)
// 			} else {
// 				conn.Close()
// 			}
// 		}
// 	}()

// 	<-done
// 	device.Disconnect()
// }

// func handleIPC(conn *net.UnixConn, device *bluetooth.Device, targetChar *bluetooth.DeviceCharacteristic) {
// 	defer conn.Close()
// 	scanner := bufio.NewScanner(conn)
// 	for scanner.Scan() {
// 		var req map[string]string
// 		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
// 			log.Printf("Error %s", err)
// 			continue
// 		}

// 		rawMsg := scanner.Bytes()

// 		msg, err, didDefaulted := ParseJsonMsgFromUnPri(rawMsg)

// 		if err != nil {
// 			log.Fatal(err)
// 			continue
// 		}

// 		if didDefaulted {
// 			log.Printf("Msg defaulted:-")
// 			log.Printf("%s", scanner.Text())
// 			continue
// 		}

// 		switch v := msg.(type) {
// 		case TestMstToPriTypeMsg:
// 			log.Printf("Logged from unprivileged app")
// 		case TestMstToMobBtTypeMsg:
// 			log.Printf("Logged from unprivileged app to mob via bt")
// 			log.Printf("Sending JSON to mobile via BT")

// 			// 1. Convert the struct back to a JSON byte array
// 			jsonData, _ := json.Marshal(v)

// 			// 2. Add an EOF marker so Android knows when to parse it
// 			jsonData = append(jsonData, '\n')

// 			// 3. Chunk and send!
// 			for i := 0; i < len(jsonData); i += 20 {
// 				end := i + 20
// 				if end > len(jsonData) {
// 					end = len(jsonData)
// 				}

// 				// Write Without Response is much faster for chunked data
// 				_, writeErr := targetChar.WriteWithoutResponse(jsonData[i:end])
// 				if writeErr != nil {
// 					log.Printf("Failed to write BT chunk: %v", writeErr)
// 					break
// 				}

// 				// Tiny delay to prevent overflowing the Bluetooth hardware buffer
// 				time.Sleep(10 * time.Millisecond)
// 			}
// 			log.Printf("Successfully sent %d bytes over BLE", len(jsonData))
// 		}

// 		// log.Printf("Msg: %s", scanner.Text())
// 		// log.Printf("req: %s", req["command"])

// 		// switch req["command"] {
// 		// case "PING":
// 		// 	conn.Write([]byte(`{"response": "PONG", "device": "connected"}` + "\n"))
// 		// case "GET_RSSI":

// 		// 	conn.Write([]byte(`{"rssi": "stable"}` + "\n"))
// 		// }
// 	}
// }
