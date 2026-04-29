from rosie_ai_agent.prompts import chat_system_prompt, extract_prompt


def test_chat_system_prompt_names_ai_identity():
    prompt = chat_system_prompt("张三理发店", "营业时间 10 点到 20 点")
    assert "AI 电话前台" in prompt
    assert "不要冒充老板本人" in prompt
    assert "张三理发店" in prompt


def test_extract_prompt_requests_json():
    prompt = extract_prompt("张三理发店", "客户想预约明天下午两点剪头发")
    assert "只输出 JSON" in prompt
    assert "appointment_time" in prompt

