#!/usr/bin/env python3
# 文件名：generate-mock-media.py
# 主要功能：通过数学波形和纯色画面生成无人物、无账号数据的脱机音视频样例。
# 作者：2guan
"""仅在维护演示素材时手动运行；日常构建直接使用仓库中的固定样例。"""
from pathlib import Path
import math
import struct
import subprocess
import wave

media_dir = Path(__file__).resolve().parents[1] / "plugins/dashboard/web/public"
# 音频是五秒钟的轻柔提示音，不是 TTS 实测结果，也不声称包含可转写的语音。
rate = 16000
with wave.open(str(media_dir / "sample-audio.wav"), "wb") as audio:
    audio.setnchannels(1)
    audio.setsampwidth(2)
    audio.setframerate(rate)
    frames = bytearray()
    for index in range(rate * 5):
        t = index / rate
        envelope = min(t * 4, 1, (5 - t) * 4)
        frames.extend(struct.pack("<h", int(2000 * envelope * math.sin(2 * math.pi * 440 * t))))
    audio.writeframes(frames)
# 固定三秒色块用于验证视频控件与布局；不需要登录、下载或提供真实视频。
subprocess.run([
    "ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i",
    "color=c=0x90ad92:s=480x270:r=15:d=3", "-an", "-c:v", "libx264",
    "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-y",
    str(media_dir / "sample-video.mp4"),
], check=True)
