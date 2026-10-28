import csv,json,shutil,subprocess,zipfile
from pathlib import Path
root=Path(__file__).resolve().parent.parent
out=root/'out/v2'
segments=[
 (0,5,'Silieco，让产品、研发和 Agent，在同一个工作空间里接力协作。'),
 (5,10,'产品成员输入需求，补齐业务目标、原始资料和验收范围。'),
 (10,14,'人完成当前工作，把 Task 交给 Agent，下一棒开始。'),
 (14,19,'Agent 可以运行在另一台机器上，接收任务并获取关联信息。'),
 (19,24,'项目目录、前序任务、SOP 要求和绑定的 Skill，一起进入执行上下文。'),
 (24,29,'Agent 按阶段要求完成设计，创建关联研发任务，继续推进实现。'),
 (29,34,'执行进展与产物回写项目，团队成员随时了解工作推进到哪一步。'),
 (34,38,'实现与本地检查完成后，Agent 将验收任务交给研发负责人。'),
 (38,42,'任务的负责人变了，需求、代码和测试记录跟着一起交接。'),
 (42,47,'研发核对交付结果，Agent 补充证据，产品成员确认业务范围。'),
 (47,53,'确认结果后，由研发完成验收，并将结论留在项目里。'),
 (53,59,'每次交接都保留上下文，让下一位成员从完整信息继续工作。'),
 (59,64,'Silieco，人和 Agent，接力完成每一次交付。')]
def stamp(sec):
 ms=round(sec*1000);return f'{ms//3600000:02d}:{ms//60000%60:02d}:{ms//1000%60:02d},{ms%1000:03d}'
captions=[{'text':s,'startMs':a*1000,'endMs':b*1000,'timestampMs':None,'confidence':None} for a,b,s in segments]
(out/'讲解字幕.json').write_text(json.dumps(captions,ensure_ascii=False,indent=2))
(out/'讲解字幕.srt').write_text('\n\n'.join(f'{i+1}\n{stamp(a)} --> {stamp(b)}\n{s}' for i,(a,b,s) in enumerate(segments))+'\n')
with (out/'配音时间轴.csv').open('w',encoding='utf-8-sig') as f:
 w=csv.writer(f);w.writerow(['开始秒','结束秒','配音文案']);w.writerows(segments)
(out/'配音文案.md').write_text('# 配音文案与时间轴\n\n'+'\n\n'.join(f'**{stamp(a)[:-4]}–{stamp(b)[:-4]}**\n\n{s}' for a,b,s in segments)+'\n')
shutil.copy2(root/'public/v2/audio-timeline.json',out/'音效卡点时间轴.json')
for source,target in [('music.wav','背景音乐.wav'),('sfx.wav','提示音效.wav')]:shutil.copy2(root/'public/v2'/source,out/target)
(out/'预览.html').write_text('''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>Silieco 人机协作宣传片</title><style>body{margin:0;padding:40px;background:#f1f5fa;color:#202b3d;font:16px system-ui}main{max-width:1280px;margin:auto}video{width:100%;border-radius:18px;box-shadow:0 20px 70px #35507120}p{color:#78869a}a{color:#3978f6}</style><main><h1>Silieco · 人和 Agent 接力协作</h1><p>64 秒 / 1080p / 30fps / 背景音乐 + 动作提示音</p><video controls preload="metadata" src="Silieco-人机协作宣传片.mp4"></video><p>产品输入 → 跨机器 Agent 执行 → 研发验收。协作流程演绎，未录制真实成员或操作项目数据。</p><p><a href="讲解字幕.srt">配音字幕</a> · <a href="配音时间轴.csv">配音时间轴</a> · <a href="音效卡点时间轴.json">音效卡点</a></p></main></html>''')
