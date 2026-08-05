package server

import "github.com/gin-gonic/gin"

func handleTranslate(c *gin.Context) {
	var req struct {
		Text       string `json:"text"`
		SourceLang string `json:"source_lang"`
		TargetLang string `json:"target_lang"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "message": err.Error()})
		return
	}
	if req.Text == "" || req.SourceLang == "" || req.TargetLang == "" {
		c.JSON(400, gin.H{"code": 400, "message": "text, source_lang and target_lang are required"})
		return
	}
	result, err := translateSvc.Translate(c.Request.Context(), req.SourceLang, req.TargetLang, req.Text)
	if err != nil {
		c.JSON(502, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "message": "ok", "data": gin.H{"translated_text": result}})
}
