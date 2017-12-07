/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

var podName string
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Ignore http origin
	},
}

func init() {
	podName = os.Getenv("podname")
	if podName == "" {
		podName = "UNKNOWN"
	}
}

func main() {
	log.Println("Starting on :8080")
	http.HandleFunc("/ws", ws)
	http.HandleFunc("/", root)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func ws(w http.ResponseWriter, r *http.Request) {
	log.Println("Received request", r.RemoteAddr)
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("failed to upgrade:", err)
		return
	}
	defer c.Close()

	s := fmt.Sprintf("Connected to %v", podName)
	if err := c.WriteMessage(websocket.TextMessage, []byte(s)); err != nil {
		log.Println("err:", err)
	}
	handleWSConn(c)
}

func handleWSConn(c *websocket.Conn) {
	stop := make(chan struct{})
	in := make(chan string)
	ticker := time.NewTicker(5 * time.Second)

	go func() {
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("Error while reading:", err)
				close(stop)
				break
			}
			in <- string(message)
		}
		log.Println("Stop reading of connection from", c.RemoteAddr())
	}()

	for {
		var msg string
		select {
		case t := <-ticker.C:
			msg = fmt.Sprintf("%s reports time: %v", podName, t.String())
		case m := <-in:
			msg = m
		case <-stop:
			break
		}

		if err := c.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
			log.Println("Error while writing:", err)
			break
		}
	}
	log.Println("Stop handling of connection from", c.RemoteAddr())
}

func root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s := struct {
		URL     string
		PodName string
	}{
		URL:     "ws://" + r.Host + "/ws",
		PodName: podName,
	}
	testPage.Execute(w, s)
}

var testPage = template.Must(template.New("").Parse(`
<!DOCTYPE html><head><title>Websocket Server</title>
<script>
function htmlize(m) {
    var line = document.createElement("div");
    line.innerHTML = m;
    result.appendChild(line)
};

window.addEventListener("load", function(evt) {
    var socket;
    var txt = document.getElementById("txt");
    var result = document.getElementById("result");

    document.getElementById("connect").onclick = function(evt) {
        if (socket) {
            return false;
        }

        socket = new WebSocket("{{.URL}}");

        socket.onopen = function(evt) {
            htmlize("<p>Connected<p>");
        }
        socket.onclose = function(evt) {
            htmlize("<p>Disconnected<p>");
            socket = null;
        }
        socket.onmessage = function(e) {
            htmlize("Received: " + e.data);
        }
        socket.onerror = function(e) {
            print("error: " + e.data);
        }

        return false;
    };

    document.getElementById("send").onclick = function(e) {
        if (!socket) {
            return false;
        }

        socket.send(txt.value);
        htmlize("Sent: " + txt.value);

        return false;
    };

    document.getElementById("disconnect").onclick = function(e) {
        if (!socket) {
            return false;
        }

        socket.close();

        return false;
    };

});
</script>
</head>
<body>
<p>Page served by pod '{{.PodName}}'</p>
<form>
	<button id="connect">Connect</button>
	<button id="disconnect">Disconnect</button>
	<input id="txt" type="text" value="">
	<button id="send">Send</button>
</form>

<div id="result"></div>
</body></html>
`))
