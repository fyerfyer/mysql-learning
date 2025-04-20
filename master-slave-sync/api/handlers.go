package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"master-slave-sync/internal/replication"
	"master-slave-sync/internal/storage"
)

// MasterHandler 主节点API处理器
type MasterHandler struct {
	Master *replication.Master
}

// SlaveHandler 从节点API处理器
type SlaveHandler struct {
	Slave *replication.Slave
}

// 请求和响应的结构体定义
type createRecordRequest struct {
	Content string `json:"content"`
}

type updateRecordRequest struct {
	Content string `json:"content"`
}

type recordResponse struct {
	ID        uint   `json:"id"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type slaveAckRequest struct {
	SlaveID  string `json:"slave_id"`
	Position uint64 `json:"position"`
}

type registerSlaveRequest struct {
	SlaveID string `json:"slave_id"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// NewMasterHandler 创建主节点API处理器
func NewMasterHandler(master *replication.Master) *MasterHandler {
	return &MasterHandler{Master: master}
}

// NewSlaveHandler 创建从节点API处理器
func NewSlaveHandler(slave *replication.Slave) *SlaveHandler {
	return &SlaveHandler{Slave: slave}
}

// SetupMasterRoutes 设置主节点的API路由
func (h *MasterHandler) SetupMasterRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// 记录处理路由
	mux.HandleFunc("/api/records", h.handleRecords)
	mux.HandleFunc("/api/records/", h.handleRecordByID)

	// 复制相关路由
	mux.HandleFunc("/api/binlog", h.handleBinlog)
	mux.HandleFunc("/api/ack", h.handleAck)
	mux.HandleFunc("/api/register_slave", h.handleRegisterSlave)

	// 状态信息路由
	mux.HandleFunc("/api/status", h.handleStatus)

	return mux
}

// SetupSlaveRoutes 设置从节点的API路由
func (h *SlaveHandler) SetupSlaveRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// 只读记录路由
	mux.HandleFunc("/api/records", h.handleRecords)
	mux.HandleFunc("/api/records/", h.handleRecordByID)

	// 状态信息路由
	mux.HandleFunc("/api/status", h.handleStatus)

	// 同步控制路由
	mux.HandleFunc("/api/sync/start", h.handleStartSync)
	mux.HandleFunc("/api/sync/stop", h.handleStopSync)

	return mux
}

// --- 主节点处理器 ---

// handleRecords 处理记录集合请求（GET：获取所有记录，POST：创建新记录）
func (h *MasterHandler) handleRecords(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// 获取所有记录
		records, err := h.Master.GetDB().ListRecords()
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		var response []recordResponse
		for _, record := range records {
			response = append(response, recordResponse{
				ID:        record.ID,
				Content:   record.Content,
				CreatedAt: record.CreatedAt.Format("2006-01-02 15:04:05"),
				UpdatedAt: record.UpdatedAt.Format("2006-01-02 15:04:05"),
			})
		}
		respondWithJSON(w, http.StatusOK, response)

	case http.MethodPost:
		// 创建记录
		var req createRecordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid request payload")
			return
		}
		defer r.Body.Close()

		record, err := h.Master.CreateRecord(req.Content)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		resp := recordResponse{
			ID:        record.ID,
			Content:   record.Content,
			CreatedAt: record.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: record.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
		respondWithJSON(w, http.StatusCreated, resp)

	default:
		respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleRecordByID 处理单条记录请求（GET：获取记录，PUT：更新记录，DELETE：删除记录）
func (h *MasterHandler) handleRecordByID(w http.ResponseWriter, r *http.Request) {
	// 从URL提取ID
	idStr := r.URL.Path[len("/api/records/"):]
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid record ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		// 获取单条记录
		record, err := h.Master.GetDB().GetRecord(uint(id))
		if err != nil {
			respondWithError(w, http.StatusNotFound, "Record not found")
			return
		}

		resp := recordResponse{
			ID:        record.ID,
			Content:   record.Content,
			CreatedAt: record.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: record.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
		respondWithJSON(w, http.StatusOK, resp)

	case http.MethodPut:
		// 更新记录
		var req updateRecordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid request payload")
			return
		}
		defer r.Body.Close()

		if err := h.Master.UpdateRecord(uint(id), req.Content); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		respondWithJSON(w, http.StatusOK, map[string]string{"message": "Record updated successfully"})

	case http.MethodDelete:
		// 删除记录
		if err := h.Master.DeleteRecord(uint(id)); err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		respondWithJSON(w, http.StatusOK, map[string]string{"message": "Record deleted successfully"})

	default:
		respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleBinlog 提供binlog条目给从节点
func (h *MasterHandler) handleBinlog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// 获取请求参数
	query := r.URL.Query()

	// 解析位置参数
	posStr := query.Get("position")
	position := uint64(0)
	if posStr != "" {
		var err error
		position, err = strconv.ParseUint(posStr, 10, 64)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid position parameter")
			return
		}
	}

	// 记录从节点ID（可选）
	slaveID := query.Get("slave_id")
	if slaveID != "" {
		log.Printf("Binlog requested by slave %s from position %d", slaveID, position)
	}

	// 获取binlog条目
	entries := h.Master.GetBinlogEntries(position)
	respondWithJSON(w, http.StatusOK, entries)
}

// handleAck 处理从节点的确认请求
func (h *MasterHandler) handleAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req slaveAckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// 记录确认信息
	h.Master.RecordSlaveACK(req.SlaveID, req.Position)
	log.Printf("Received ACK from slave %s for position %d", req.SlaveID, req.Position)

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "ACK received"})
}

// handleRegisterSlave 处理从节点注册请求
func (h *MasterHandler) handleRegisterSlave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req registerSlaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	// 注册从节点
	h.Master.RegisterSlave(req.SlaveID, req.Host, req.Port)

	respondWithJSON(w, http.StatusOK, map[string]string{
		"status":   "Slave registered successfully",
		"slave_id": req.SlaveID,
	})
}

// handleStatus 返回主节点状态信息
func (h *MasterHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	stats := h.Master.GetStats()
	respondWithJSON(w, http.StatusOK, stats)
}

// --- 从节点处理器 ---

// handleRecords 处理只读记录请求
func (h *SlaveHandler) handleRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondWithError(w, http.StatusMethodNotAllowed, "Only read operations allowed on slave")
		return
	}

	// 获取所有记录
	db := h.Slave.GetDB()
	records, err := db.ListRecords()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var response []recordResponse
	for _, record := range records {
		response = append(response, recordResponse{
			ID:        record.ID,
			Content:   record.Content,
			CreatedAt: record.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: record.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	respondWithJSON(w, http.StatusOK, response)
}

// handleRecordByID 处理只读单条记录请求
func (h *SlaveHandler) handleRecordByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondWithError(w, http.StatusMethodNotAllowed, "Only read operations allowed on slave")
		return
	}

	// 从URL提取ID
	idStr := r.URL.Path[len("/api/records/"):]
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid record ID")
		return
	}

	// 获取单条记录
	db := h.Slave.GetDB()
	record, err := db.GetRecord(uint(id))
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Record not found")
		return
	}

	resp := recordResponse{
		ID:        record.ID,
		Content:   record.Content,
		CreatedAt: record.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt: record.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
	respondWithJSON(w, http.StatusOK, resp)
}

// handleStatus 返回从节点状态信息
func (h *SlaveHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	stats := h.Slave.GetStats()
	respondWithJSON(w, http.StatusOK, stats)
}

// handleStartSync 启动同步进程
func (h *SlaveHandler) handleStartSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	h.Slave.StartSync()
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "Sync started"})
}

// handleStopSync 停止同步进程
func (h *SlaveHandler) handleStopSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	h.Slave.StopSync()
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "Sync stopped"})
}

// --- 工具函数 ---

// respondWithError 返回错误响应
func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, errorResponse{Error: message})
}

// respondWithJSON 返回JSON响应
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("JSON marshaling error: %v", err)))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

// GetDB 扩展Master和Slave以获取内部DB实例
func (h *MasterHandler) GetDB() *storage.DB {
	return h.Master.GetDB()
}

func (h *SlaveHandler) GetDB() *storage.DB {
	return h.Slave.GetDB()
}
