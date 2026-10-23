//! 托盘图标：运行时画出来的 RGBA 圆点，不依赖图片资源文件。
//!
//! 六种状态用颜色区分（需求 2.1、4.8）；「重启中」画空心环，和其余实心状态在
//! 形状上也能分开，色弱用户不必只靠颜色判断。

use crate::state::IconState;
use tray_icon::Icon;

const SIZE: u32 = 32;

fn color(state: IconState) -> [u8; 3] {
    match state {
        IconState::Ok => [0x2e, 0xa0, 0x43],
        // 黄色专给证书提醒；维护改用紫色，免得两种「不是故障」的状态在菜单栏里分不清。
        IconState::Warning => [0xf2, 0xc0, 0x1d],
        IconState::Error => [0xd7, 0x3a, 0x49],
        IconState::Maintenance => [0x8b, 0x5c, 0xf6],
        IconState::Restarting => [0x3b, 0x82, 0xf6],
        IconState::Down => [0x8b, 0x8f, 0x96],
    }
}

/// 生成指定状态的图标。
pub fn icon(state: IconState) -> Icon {
    let [r, g, b] = color(state);
    let hollow = state == IconState::Restarting;
    let c = (SIZE as f32 - 1.0) / 2.0;
    let outer = SIZE as f32 * 0.42;
    let inner = if hollow { outer - 4.0 } else { -1.0 };
    let mut px = Vec::with_capacity((SIZE * SIZE * 4) as usize);
    for y in 0..SIZE {
        for x in 0..SIZE {
            let d = ((x as f32 - c).powi(2) + (y as f32 - c).powi(2)).sqrt();
            // 边缘一像素做线性抗锯齿。
            let a_out = (outer + 0.5 - d).clamp(0.0, 1.0);
            let a_in = (d - inner + 0.5).clamp(0.0, 1.0);
            let a = (a_out * a_in * 255.0) as u8;
            px.extend_from_slice(&[r, g, b, a]);
        }
    }
    Icon::from_rgba(px, SIZE, SIZE).expect("固定尺寸的 RGBA 缓冲区")
}
