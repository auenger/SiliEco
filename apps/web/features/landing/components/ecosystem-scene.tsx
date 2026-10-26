// Adapted from promo/src/ecosystem-ending.tsx: original portraits, workflow layout and timing.
import React from "react";
import {
  Img,
  staticFile,
  interpolate,
  Easing,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";

const C = {
  ink: "var(--home-ink)",
  muted: "var(--home-muted)",
  line: "var(--home-line)",
  blue: "var(--home-accent)",
};
const clamp = {
  extrapolateLeft: "clamp" as const,
  extrapolateRight: "clamp" as const,
};
const curve = Easing.bezier(0.22, 0.8, 0.24, 1);
const p = (t: number, a: number, d = 0.7) =>
  interpolate(t, [a, a + d], [0, 1], { ...clamp, easing: curve });
type Member = {
  file: string;
  role: string;
  kind: "human" | "agent";
  zoom?: number;
};
type Flow = {
  name: string;
  color: string;
  x: number;
  y: number;
  stages: string[];
  members: Member[];
};
const flows: Flow[] = [
  {
    name: "系统研发 SOP",
    color: "#ed725e",
    x: 100,
    y: 300,
    stages: ["需求整理", "研发实现", "测试验收"],
    members: [
      { file: "product", role: "产品", kind: "human", zoom: 1.2 },
      { file: "agent4", role: "研发 Agent", kind: "agent", zoom: 1.05 },
      { file: "developer", role: "研发负责人", kind: "human", zoom: 1.2 },
    ],
  },
  {
    name: "内容生产 SOP",
    color: "#c9988e",
    x: 1210,
    y: 300,
    stages: ["创意策划", "内容制作", "发布确认"],
    members: [
      { file: "human1", role: "内容策划", kind: "human", zoom: 1.1 },
      { file: "agent1", role: "内容 Agent", kind: "agent", zoom: 1.4 },
      { file: "human2", role: "内容负责人", kind: "human", zoom: 1.15 },
    ],
  },
  {
    name: "客户服务 SOP",
    color: "#a2af91",
    x: 100,
    y: 650,
    stages: ["问题受理", "分析处理", "服务确认"],
    members: [
      { file: "human3", role: "客户服务", kind: "human", zoom: 1.35 },
      { file: "agent2", role: "服务 Agent", kind: "agent", zoom: 1.25 },
      { file: "human4", role: "业务专家", kind: "human", zoom: 1.1 },
    ],
  },
  {
    name: "数据分析 SOP",
    color: "#c3a774",
    x: 1210,
    y: 650,
    stages: ["业务提问", "数据分析", "结论确认"],
    members: [
      { file: "human2", role: "业务分析", kind: "human", zoom: 1.15 },
      { file: "agent3", role: "分析 Agent", kind: "agent", zoom: 1.4 },
      { file: "human1", role: "业务负责人", kind: "human", zoom: 1.1 },
    ],
  },
];

function Portrait({
  member,
  color,
  active,
}: {
  member: Member;
  color: string;
  active: boolean;
}) {
  return (
    <div
      style={{
        width: 62,
        height: 62,
        position: "relative",
        borderRadius: member.kind === "human" ? 99 : 17,
        border: `2px solid ${active ? color : "var(--home-line)"}`,
        background: "var(--home-panel)",
        boxShadow: active ? `0 0 0 5px ${color}16` : "0 3px 12px #29466b10",
      }}
    >
      <div
        style={{
          position: "absolute",
          inset: 2,
          overflow: "hidden",
          borderRadius: member.kind === "human" ? 99 : 14,
        }}
      >
        <Img
          src={staticFile(`media/ecosystem/avatars/${member.file}.jpg`)}
          style={{
            width: "100%",
            height: "100%",
            objectFit: "cover",
            scale: member.zoom ?? 1,
          }}
        />
      </div>
      <span
        style={{
          position: "absolute",
          right: -4,
          bottom: -4,
          width: 19,
          height: 19,
          border: "3px solid var(--home-panel)",
          borderRadius: member.kind === "human" ? 99 : 6,
          background: member.kind === "human" ? "var(--home-human)" : C.blue,
        }}
      />
    </div>
  );
}

function Workflow({
  flow,
  index,
  t,
  compact = false,
}: {
  flow: Flow;
  index: number;
  t: number;
  compact?: boolean;
}) {
  const reveal = index === 0 ? 1 : p(t, 1.2 + index * 0.3, 0.7);
  const run = Math.max(0, t - 3.5 - index * 0.5);
  const phase = (run % 5.5) / 5.5;
  const active = phase < 0.23 ? 0 : phase < 0.72 ? 1 : 2;
  const step = p(phase, 0.12, 0.73);
  return (
    <div
      style={{
        position: "absolute",
        left: flow.x,
        top: flow.y,
        width: 610,
        height: compact ? 280 : 240,
        background: "var(--home-panel)",
        border: `1px solid ${flow.color}38`,
        borderRadius: 3,
        padding: 24,
        boxShadow: "none",
        opacity: reveal,
        translate: `0px ${28 * (1 - reveal)}px`,
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <div
          style={{
            fontSize: compact ? 28 : 25,
            fontWeight: 600,
            display: "flex",
            alignItems: "center",
            gap: 11,
          }}
        >
          <span
            style={{
              width: 9,
              height: 24,
              background: flow.color,
              borderRadius: 5,
            }}
          />
          {flow.name}
        </div>
        <div style={{ fontSize: 15, color: flow.color }}>● 协作进行中</div>
      </div>
      <svg
        width="610"
        height="180"
        style={{ position: "absolute", left: 0, top: compact ? 69 : 53 }}
      >
        <path d="M103 80H507" stroke={C.line} strokeWidth="2" />
        <path
          d="M103 80H507"
          pathLength="1"
          stroke={flow.color}
          strokeWidth="2"
          strokeDasharray="1"
          strokeDashoffset={1 - step}
        />
        <circle cx={103 + step * 404} cy="80" r="5" fill={flow.color} />
      </svg>
      {flow.members.map((member, i) => (
        <div
          key={i}
          style={{
            position: "absolute",
            left: 32 + i * 202,
            top: compact ? 80 : 75,
            width: 142,
            display: "flex",
            alignItems: "center",
            flexDirection: "column",
          }}
        >
          <div
            style={{
              fontSize: compact ? 22 : 16,
              color: C.muted,
              marginBottom: 12,
            }}
          >
            {flow.stages[i]}
          </div>
          <Portrait
            member={member}
            color={flow.color}
            active={active === i && run > 0}
          />
          <div
            style={{
              fontSize: compact ? 24 : 18,
              fontWeight: 550,
              marginTop: 9,
            }}
          >
            {member.role}
          </div>
          <div
            style={{
              fontSize: compact ? 18 : 13,
              color: member.kind === "human" ? "var(--home-human)" : C.blue,
              marginTop: 3,
            }}
          >
            {member.kind === "human" ? "人类员工" : "数字员工"}
          </div>
        </div>
      ))}
      <div
        style={{
          position: "absolute",
          left: 85 + step * 404,
          top: compact ? 103 : 83,
          opacity: run > 0 ? Math.sin(Math.PI * step) * 0.9 : 0,
          background: flow.color,
          color: "white",
          fontSize: 12,
          borderRadius: 5,
          padding: "3px 7px",
        }}
      >
        Task
      </div>
    </div>
  );
}

export function EcosystemScene({
  compact = false,
  light = true,
}: {
  compact?: boolean;
  light?: boolean;
}) {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const q = 6 + frame / fps;
  const positioned = flows.map((flow, index) => ({
    ...flow,
    color: light
      ? ["#b8402e", "#986b75", "#59734d", "#94682d"][index]!
      : flow.color,
    x: compact ? 75 : flow.x,
    y: compact ? 230 + index * 310 : flow.y - 270,
  }));
  const hubX = compact ? 230 : 810;
  const hubY = compact ? 20 : 239;
  return (
    <div
      data-ecosystem-frame={frame}
      style={{
        position: "absolute",
        inset: 0,
        color: C.ink,
        fontFamily: 'Arial, "PingFang SC", sans-serif',
        background: "var(--home-paper)",
      }}
    >
      <svg
        width={compact ? 760 : 1920}
        height={compact ? 1510 : 680}
        style={{ position: "absolute" }}
        aria-hidden="true"
      >
        {positioned.map((flow, i) => {
          const left = i % 2 === 0;
          const x1 = compact ? 230 : left ? 810 : 1110,
            y1 = compact ? 101 : 320,
            x2 = compact ? 75 : left ? 710 : 1210,
            y2 = flow.y + 120;
          const cx1 = compact ? 35 : left ? 760 : 1160,
            cx2 = compact ? 35 : left ? 770 : 1150;
          const d = `M${x1} ${y1}C${cx1} ${y1} ${cx2} ${y2} ${x2} ${y2}`;
          const packet = ((q - 3 - i * 0.45) % 4) / 4;
          const x =
            (1 - packet) ** 3 * x1 +
            3 * (1 - packet) ** 2 * packet * cx1 +
            3 * (1 - packet) * packet ** 2 * cx2 +
            packet ** 3 * x2;
          const y =
            (1 - packet) ** 3 * y1 +
            3 * (1 - packet) ** 2 * packet * y1 +
            3 * (1 - packet) * packet ** 2 * y2 +
            packet ** 3 * y2;
          return (
            <g key={flow.name}>
              <path
                d={d}
                fill="none"
                stroke={flow.color + "70"}
                strokeWidth="2"
              />
              <circle cx={x} cy={y} r="5" fill={flow.color} />
            </g>
          );
        })}
      </svg>
      {positioned.map((flow, i) => (
        <Workflow
          key={flow.name}
          flow={flow}
          index={i}
          t={q}
          compact={compact}
        />
      ))}
      <div
        style={{
          position: "absolute",
          left: hubX,
          top: hubY,
          width: 300,
          height: 162,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          background: "var(--home-panel)",
          border: "1px solid #6d5146",
          borderRadius: 3,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 13 }}>
          <Img
            src={staticFile("media/ecosystem/logo.svg")}
            style={{ width: 52, height: 52 }}
          />
          <span style={{ fontSize: 41, fontWeight: 550, letterSpacing: -1.5 }}>
            Silieco
          </span>
        </div>
        <div style={{ fontSize: 18, color: C.muted, marginTop: 14 }}>
          项目 · 任务 · Skill · 上下文
        </div>
      </div>
    </div>
  );
}
