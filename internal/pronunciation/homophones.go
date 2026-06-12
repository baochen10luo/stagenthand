package pronunciation

// HomophoneMap maps characters to their homophones (same pronunciation, different character).
// This is used to suggest replacements when TTS mispronounces a character.
// Key = character that might be mispronounced
// Value = slice of homophone replacements that TTS can pronounce correctly
type HomophoneMap map[string][]string

// DefaultHomophones returns a built-in homophone map for common Taiwanese Mandarin characters.
func DefaultHomophones() HomophoneMap {
	return HomophoneMap{
		// Common mispronunciations in Qwen3 TTS
		"公": {"工", "功", "攻", "宮"},
		"角": {"覺", "腳", "訣"},
		"色": {"瑟", "塞"},
		"樂": {"勒", "崍"},
		"供": {"工", "公", "功"},
		"瞭": {"了"},
		"解": {"姐", "街"},
		"的": {"底", "地", "得"},
		"什": {"十", "石", "時"},
		"簡": {"檢", "撿"},
		"單": {"丹", "擔"},
		"子": {"紫", "籽"},
		"大": {"打", "答"},
		"小": {"曉", "筱"},
		"中": {"忠", "鍾", "衷"},
		"國": {"果", "裹"},
		"人": {"仁", "任"},
		"生": {"聲", "昇"},
		"心": {"新", "辛", "芯"},
		"家": {"佳", "嘉"},
		"時": {"十", "石", "實"},
		"可": {"克", "刻", "客"},
		"以": {"已", "椅"},
		"在": {"再", "載"},
		"有": {"友", "酉"},
		"上": {"尚", "裳"},
		"下": {"夏", "嚇"},
		"年": {"粘", "碾"},
		"老": {"姥", "佬"},
		"頭": {"投", "骰"},
		"身": {"申", "伸", "深"},
		"很": {"狠", "痕"},
		"能": {"龍", "嚨"},
		"他": {"她", "它", "祂"},
		"們": {"門"},
		"也": {"冶", "野"},
		"就": {"舅", "舊"},
		"而": {"兒", "爾"},
		"會": {"彙", "惠", "穢"},
		"為": {"位", "未", "味"},
		"目": {"木", "沐", "睦"},
		"音": {"因", "陰", "殷"},
		"快": {"塊", "筷"},
		"因": {"音", "陰", "殷"},
		"提": {"題", "蹄"},
		"麼": {"默", "墨"},
		// Tones can be off; offer same-tone alternatives
		"是": {"事", "市", "式", "示", "世"},
		"不": {"布", "步", "部", "簿"},
		"個": {"各", "葛"},
		"這": {"者", "褶"},
		"那": {"納", "娜"},
		"都": {"嘟", "督"},
		"和": {"合", "盒", "河"},
		"了": {"瞭", "潦"},
		"著": {"者", "摺", "褶"},
		"還": {"孩", "骸"},
		"一": {"衣", "依", "醫"},

		// Characters appearing in bloodline_gangster_legacy mismatches
		"電": {"店", "殿", "佃"},
		"總": {"縱", "粽", "綜"},
		"細": {"戲", "系", "係"},
		"伯": {"博", "泊", "勃", "帛"},
		"帶": {"代", "袋", "戴", "貸"},
		"股": {"古", "鼓", "骨", "谷"},
		"狠": {"很", "痕", "懇"},
		"勁": {"近", "進", "禁", "晉"},
		"機": {"基", "雞", "激", "積"},
		"輸": {"書", "舒", "蔬", "梳"},
		"陳": {"晨", "辰", "塵", "臣"},
		"盤": {"磐", "蟠", "胖"},
		"奇": {"騎", "旗", "齊", "岐"},
		"蹟": {"跡", "際", "計", "季"},
		"般": {"搬", "班", "斑", "頒"},
		"留": {"流", "劉", "瘤", "榴"},
		"賭": {"堵", "睹", "篤"},
		"祖": {"組", "阻", "租", "卒"},
		"輩": {"被", "倍", "背", "備"},
		"墾": {"肯", "啃", "懇", "墾"},
		"債": {"寨", "在", "再", "載"},
		"兵": {"冰", "并", "斌", "檳"},
		"沉": {"陳", "晨", "辰", "塵"},
		"淪": {"倫", "輪", "綸", "掄"},
		"同": {"童", "銅", "桐", "彤"},
		"樣": {"漾", "恙", "烊"},
		"氾": {"泛", "範", "范", "梵"},
		"濫": {"爛", "瀾", "蘭"},
		"於": {"于", "余", "予", "瑜"},
		"曾": {"增", "層", "贈"},
		"兒": {"而", "爾", "耳", "邇"},
		"四": {"世", "事", "是", "市"},
	}
}

// Lookup returns homophone replacements for a character.
func (m HomophoneMap) Lookup(char string) []string {
	return m[char]
}

// Has returns true if the character has homophone entries.
func (m HomophoneMap) Has(char string) bool {
	_, ok := m[char]
	return ok
}
