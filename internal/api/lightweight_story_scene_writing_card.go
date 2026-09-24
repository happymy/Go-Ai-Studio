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
- characters：本场出场角色名列表，如 ["沈西风","王五"]，只写 existing_characters 与本集 characters 里登记过的人
- 禁止在 image_prompt 的"场景"标签里写与 location 冲突的内容；同一地点再次出现时，沿用之前 location 写过的关键陈设与光线，禁止换一套描述。`

	cinematicCard := `
【镜头语言卡与状态衔接】
- shot_size：本场景别（远景/全景/中景/近景/特写），有明确说话人时说话者应获得中景及以上清晰度
- camera_angle：本场机位视角（平视/仰视/俯视/侧拍/过肩等），仰/俯必须服务于情绪或权力关系
- camera_movement：本场运镜（固定/推/拉/摇/移/跟随等），每场只允许一种核心镜头运动
- blocking：本场人物站位与调度（一句，如"沈西风居左前景，李三居右后景"），与机位配合锁死空间关系
- ending_state：本场结束时的人物位置/持物/服装/情绪状态快照（一句），它是下一场生成的起点
- 状态衔接：第 N 场的起点必须承接第 N-1 场 ending_state（人物位置、持物、服装状态、场景状态自然延续），禁止跨场凭空重置；全剧最后一场的 ending_state 要与 episode_memory.ending_state 保持一致。`

	strictRule := `【场景写作卡·强制项】当前链路对 objective、turn、location、shot_size、camera_angle、camera_movement、blocking、ending_state 八个字段为必填：每个 scene 的这八个字段都不能为空；conflict 允许纯过渡场留空。ending_state 必须承接上一场结尾，且全剧最后一场的 ending_state 要与 episode_memory.ending_state 一致。`
	if strict {
		return strings.TrimSpace(common + "\n" + cinematicCard + "\n" + strictRule)
	}
	return strings.TrimSpace(common + cinematicCard)
}
