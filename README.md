# **IoT Agent**  

A modular and configurable **IoT agent** designed to support a variety of services and run on any Unix-based system. It enables secure communication and interaction with an MQTT backend. You can use [IOT-Cloud](https://github.com/BenMeehan/iot-cloud) or build your own MQTT backend.

🛠 **Pre-Alpha Discussions**: [Here](https://github.com/BenMeehan/iot-agent/discussions/6)  

## **📌 Features**  

✔️ Modular service-based architecture  
✔️ Secure communication via MQTT  
✔️ Supports multiple services (Registration, Metrics, SSH, Updates, etc.)  
✔️ Configurable through YAML files  
✔️ Designed for low-resource IoT devices  

## **🚀 Installation & Setup**  

### **1. Clone the Repository**  
```sh
git clone https://github.com/BenMeehan/iot-agent.git
cd iot-agent
```

### **2. Configure the Agent**  
Modify `config/config.yaml` to suit your needs. Ensure correct MQTT broker settings and service configurations.

### **3. Run the Agent**  
```sh
go run cmd/agent/main.go
```

---

## **🔧 Configuration**  
The agent is configured via `config/config.yaml`. Each service has its own parameters, such as MQTT topics, intervals, and authentication details.  

For detailed service-specific documentation, **check the [`/docs`](./docs/) folder**.

---

## **🛠 TODO**  

- [ ] **Cross-Compilation**: Easier compilation for different architectures  

---

## **📌 Architecture**  

![arch.png](./.github/images/agent-arch.png)  

---

## **⚙️ Adding a New Service**  

1. **Update Configuration**  
   - Add necessary configurations in `config/config.yaml`.  

2. **Create Service Logic**  
   - Add a new file in `internal/services` (e.g., `new_service.go`).  
   - Implement the service logic similar to existing services (e.g., `heartbeat_service.go`).  

---

## **📖 Services Overview**  

📌 **Detailed service documentation is available in the [`/docs`](./docs/) directory.**  

### **1️⃣ Registration Service**  
🔹 Handles secure device registration via MQTT using JWT authentication.  
🔹 Implements exponential backoff for retries.  

### **2️⃣ Heartbeat Service**  
🔹 Sends periodic heartbeat messages to indicate device activity.  

### **3️⃣ Metrics Service**  
🔹 Collects system metrics (CPU, memory, disk usage) and sends them via MQTT.  

### **4️⃣ Command Service**  
🔹 Executes commands on the IoT device and publishes output via MQTT.  

### **5️⃣ Geolocation Service**  
🔹 Retrieves device location via GPS or Google Geolocation API.  

### **6️⃣ SSH Service**  
🔹 Establishes a **secure reverse SSH tunnel** for remote access.  

### **7️⃣ Update Service**  
🔹 Handles OTA (Over-the-Air) updates for firmware or software.  

---

## **🔍 Code Guidelines**  

### **1. Naming Conventions**  
- Use camel case for variables/constants (e.g., `deviceId`, `maxRetries`).  
- Use snake case for files/folders (e.g., `heartbeat_service.go`).  

### **2. Code Style**  
- Keep code clean and readable.  
- Comments should explain **why** something is done, not just **what** it does.  

### **3. Logging**  
- Use structured logs with relevant context.  
- Ensure logs are useful for debugging.  

---

## **🤝 Contributing**  

Contributions are welcome! To contribute:  

1. **Fork the repository**  
2. **Create a feature branch** (`feature/your-feature-name`)  
3. **Commit your changes** (`git commit -m "Added new feature"`)  
4. **Push to your branch**  
5. **Open a Pull Request**  

---

This project is under [Apache License 2.0](./LICENSE)