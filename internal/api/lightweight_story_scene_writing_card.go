package api

import "strings"

// buildSceneWritingCard 返回追加到 4 种模式 systemPrompt 末尾的"场景写作卡"。
// strict=true 用于 h3_short 链路：objective/turn/location 对该模式为必填（校验层软告警）；
// 其余模式保证"每场有戏"，但允许过渡场 conflict 留空（turn 仍必写）。
func buildSceneWritingCard(strict bool) string {
	common := `【场景写作卡】每一个 scene 都必须有"戏"，不能只是平铺直叙的过场：
- objective：本场主角要什么（一句话，必写）
- conflict：本场遇到的障碍或对抗（一句话；纯过渡场允许留空，但留空时必须写清 turn）
- turn：本场结束时状态、情绪或关系发生的变化（一句话，必写）
- scene_function：本场承担的镜头功能，从下面六个中选一个：建立、关系、行为、反应、信息揭露、收束
- mood_arc：本场情绪弧，如"平静→紧张"，可空
- location：本场地点资产（一句话，如"客栈大堂 内 夜，木桌油灯，桌面摆着酒碗"），独立于 image_prompt 单独成字段
- 禁止在 image_prompt 的"场景"标签里写与 location 冲突的内容；同一地点再次出现时，沿用之前 location 写过的关键陈设与光线，禁止换一套描述。`

	strictRule := `【场景写作卡·强制项】当前链路对 objective、turn、location 三个字段为必填：每个 scene 的 objective、turn、location 都不能为空；conflict 允许纯过渡场留空。`
	if strict {
		return strings.TrimSpace(common + "\n" + strictRule)
	}
	return strings.TrimSpace(common)
}
