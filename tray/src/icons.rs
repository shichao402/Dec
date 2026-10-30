//! 托盘图标：运行时把品牌字标画成 RGBA，不依赖图片资源文件。
//!
//! 五种状态共用同一枚品牌图形，只用颜色区分（需求 2.1）。形状完全恒定，
//! 状态切换时图标不会「跳」，扫一眼菜单栏就知道是同一个程序、只是有事没事。
//!
//! 绘制分辨率刻意高于菜单栏的显示尺寸（macOS 22pt、Windows 16/20px）：
//! 品牌图形线条细，按显示尺寸直接画会让笔画落不满一个像素，缝隙糊在一起。
//! 交给系统做缩放，等效清晰度更高。

use crate::logo_polygons::CONTOURS;
use crate::state::IconState;
use tray_icon::Icon;

/// 绘制边长。显示时系统会缩到 22pt 左右，这里的余量用于保留缝隙细节。
pub const SIZE: u32 = 64;

/// 品牌图形在 1024 画布上的包围盒，用来裁掉四周留白，让小尺寸下图形更大。
/// 与 [`CONTOURS`] 同源，值由生成脚本算出。
const BBOX: (f32, f32, f32, f32) = (98.0, 96.0, 926.0, 928.0);

/// 裁掉留白后，图形四周再留出的边距（占图形边长比例）。
const MARGIN: f32 = 0.04;

/// 每像素的子采样数（每轴）。
const SUBSAMPLES: u32 = 4;

/// 品牌墨色。取自品牌图标的原始填充色，是这套标识的本色。
const INK: [u8; 3] = [0x22, 0x20, 0x1e];

/// 品牌米白。品牌图标的原始底色，在这里用作墨色主体的描边。
const BONE: [u8; 3] = [0xf2, 0xef, 0xe9];

/// 状态色。正常态直接用品牌本色，其余四态取明亮且色相分得开的一组。
///
/// 菜单栏是半透明材质，底色随壁纸变化。正常态若用深色会融进深色菜单栏，
/// 所以它的可见性靠 [`rim`] 的米白描边兜住：浅底上墨色主体立住轮廓，
/// 深底上描边反过来勾出形状，两个方向都至少有一层能拉开对比。
fn color(state: IconState) -> [u8; 3] {
    match state {
        IconState::Ok => INK,
        IconState::Error => [0xff, 0x7a, 0x6b],
        IconState::Maintenance => [0xc0, 0x9c, 0xff],
        IconState::Restarting => [0x6c, 0xc0, 0xff],
        IconState::Down => [0xc9, 0xc5, 0xbc],
    }
}

/// 图形外缘的描边色。亮色主体在浅色壁纸下会融进背景，
/// 加一圈近黑描边后，任何底色都至少有一层能拉开对比。
const RIM_DARK: [u8; 3] = [0x1c, 0x1a, 0x18];

/// 状态对应的描边色。
///
/// 描边必须与主体拉开明度差，否则整枚图标会糊成一个实心块、叶片之间的
/// 缝隙也就消失了。正常态主体是墨色，描边因此改用米白；其余四态主体都是
/// 高明度色，沿用近黑描边。
fn rim(state: IconState) -> [u8; 3] {
    match state {
        IconState::Ok => BONE,
        _ => RIM_DARK,
    }
}

/// 描边宽度（相对图形边长的比例）。太粗会把叶片之间的窄缝糊死，
/// 太细在小尺寸下又看不出来，0.015 是两者之间的平衡点。
const RIM_WIDTH: f32 = 0.015;

/// 生成指定状态的图标。
pub fn icon(state: IconState) -> Icon {
    Icon::from_rgba(rgba(state), SIZE, SIZE).expect("固定尺寸的 RGBA 缓冲区")
}

/// 指定状态的 RGBA 像素，尺寸 [`SIZE`]。
pub fn rgba(state: IconState) -> Vec<u8> {
    rgba_at(state, 0.0)
}

/// 指定状态、按 `degrees` 顺时针旋转后的 RGBA 像素。
///
/// 只有「重启中」会用到非零角度：其它状态的形状必须纹丝不动，
/// 否则图标在状态变化之间跳来跳去。
pub fn rgba_at(state: IconState, degrees: f32) -> Vec<u8> {
    rasterize(color(state), rim(state), SIZE, degrees)
}

/// 指定状态、按 `degrees` 顺时针旋转后的图标。
pub fn spin_icon_at(state: IconState, degrees: f32) -> Icon {
    Icon::from_rgba(rgba_at(state, degrees), SIZE, SIZE).expect("固定尺寸的 RGBA 缓冲区")
}

/// 旋转一圈需要多少帧。[`spin_frames`] 与它配合给出离散角度。
pub const SPIN_FRAMES: u32 = 24;

/// 第 `frame` 帧的旋转角度（度）。整圈取整便于匀速循环。
pub fn spin_frames(frame: u32) -> f32 {
    (frame % SPIN_FRAMES) as f32 * 360.0 / SPIN_FRAMES as f32
}

/// 预渲染整圈旋转帧，供动画循环取用。
///
/// 单帧光栅化要 20 毫秒以上，直接在 UI 线程每帧现算会拖慢托盘。所以进
/// 「重启中」时一次性把这些帧算好（约半秒），之后每帧只是换一张现成的位图。
pub fn spin_frames_ready() -> Vec<Icon> {
    (0..SPIN_FRAMES)
        .map(|f| spin_icon_at(IconState::Restarting, spin_frames(f)))
        .collect()
}

/// 把品牌轮廓光栅化成 `size × size` 的 RGBA，顺时针旋转 `degrees` 度。
///
/// 轮廓是若干闭合多边形，按 nonzero 环绕数判断内外：环绕数不为 0 即实心，
/// 因此内圈与叶片之间的空隙会自然成为镂空，无需单独描述。
fn rasterize(rgb: [u8; 3], rim_rgb: [u8; 3], size: u32, degrees: f32) -> Vec<u8> {
    let [r, g, b] = rgb;
    // 把包围盒放大成正方形并等比缩放，居中后铺满整个位图。
    let (x0, y0, x1, y1) = BBOX;
    let span = (x1 - x0).max(y1 - y0) * (1.0 + MARGIN * 2.0);
    let cx = (x0 + x1) / 2.0;
    let cy = (y0 + y1) / 2.0;
    let scale = size as f32 / span;
    let half = size as f32 / 2.0;

    // 逆向旋转采样：把屏幕采样点转回原图坐标再判断内外，比正向变换图形简单。
    // 旋转后图形四角可能顶出位图，所以还要按比例缩小一点避免被裁掉。
    let rad = -degrees.to_radians();
    let (sin, cos) = rad.sin_cos();
    let inv_zoom = 1.0 / spin_zoom(degrees);

    // 描边按原图坐标处理，换算成原图单位。
    let rim_unit = RIM_WIDTH * span;
    // 每个像素记两份覆盖率：主体（含描边）与仅主体。
    let mut ink_buf = vec![0.0f32; (size * size) as usize];
    let mut core_buf = vec![0.0f32; (size * size) as usize];

    let w = 1.0 / (SUBSAMPLES * SUBSAMPLES) as f32;
    for sy in 0..size * SUBSAMPLES {
        let sample_y = (sy as f32 + 0.5) / SUBSAMPLES as f32;
        let dy = (sample_y - half) / scale * inv_zoom;
        let row = sy / SUBSAMPLES;
        let base = (row * size) as usize;
        for sx in 0..size * SUBSAMPLES {
            let sample_x = (sx as f32 + 0.5) / SUBSAMPLES as f32;
            let dx = (sample_x - half) / scale * inv_zoom;
            let px = cx + dx * cos - dy * sin;
            let py = cy + dx * sin + dy * cos;
            let col = base + (sx / SUBSAMPLES) as usize;
            if in_shape(px, py) {
                core_buf[col] += w;
            } else if near_shape(px, py, rim_unit) {
                // 图形外部、但贴着图形的点：既包括整体外缘，也包括叶片之间的
                // 缝隙。两侧都有墨迹的缝隙会被两个方向都判定，照样描边。
                ink_buf[col] += w;
            }
        }
    }

    let [rr, rg, rb] = rim_rgb;
    let mut buf = Vec::with_capacity((size * size * 4) as usize);
    for i in 0..ink_buf.len() {
        let core = core_buf[i].min(1.0);
        let rim = (ink_buf[i] + core).min(1.0);
        // 主体与描边按覆盖率合成：主体优先，露出的是描边。
        let (cr, cg, cb) = if core >= rim { (r, g, b) } else { (rr, rg, rb) };
        buf.extend_from_slice(&[cr, cg, cb, (rim * 255.0).round() as u8]);
    }
    buf
}

/// 点是否落在图形内（nonzero 环绕数判定）。
fn in_shape(px: f32, py: f32) -> bool {
    winding(px, py) != 0
}

/// 点是否贴着图形外缘 `distance` 以内（用于描边）。
fn near_shape(px: f32, py: f32, distance: f32) -> bool {
    const DIRS: [(f32, f32); 8] = [
        (1.0, 0.0),
        (-1.0, 0.0),
        (0.0, 1.0),
        (0.0, -1.0),
        (0.7071, 0.7071),
        (-0.7071, 0.7071),
        (0.7071, -0.7071),
        (-0.7071, -0.7071),
    ];
    DIRS.iter()
        .any(|(dx, dy)| in_shape(px + dx * distance, py + dy * distance))
}

/// nonzero 环绕数：正负方向都算「在内」，只有净计数为零才算外部。
fn winding(px: f32, py: f32) -> i32 {
    let mut wind = 0;
    for contour in CONTOURS {
        let n = contour.len();
        for i in 0..n {
            let (x1, y1) = contour[i];
            let (x2, y2) = contour[(i + 1) % n];
            let (x1, y1) = (x1 as f32, y1 as f32);
            let (x2, y2) = (x2 as f32, y2 as f32);
            // 半开区间 [y1, y2) 避免顶点被相邻两条边重复计入
            if (y1 <= py) != (y2 <= py) {
                let t = (py - y1) / (y2 - y1);
                if x1 + t * (x2 - x1) > px {
                    wind += if y2 > y1 { 1 } else { -1 };
                }
            }
        }
    }
    wind
}

/// 旋转时把图形缩小多少才能不出界。
///
/// 品牌图形的主体是那个外圆，而圆是旋转不变的。若按矩形包围盒补偿，
/// 每个角度都要缩到最坏情况（40% 以上），菜单栏里看就是图标在「呼吸」。
/// 这里以图形中心到最远墨迹点的距离为半径做圆形预算：位图方形内切圆
/// 即可容纳整圈旋转，任何角度都不必再缩，图形大小恒定。
fn spin_zoom(degrees: f32) -> f32 {
    if degrees == 0.0 {
        return 1.0;
    }
    // 内切圆半径 = 可用方形边长的一半。
    let (x0, y0, x1, y1) = BBOX;
    let usable = (x1 - x0).max(y1 - y0);
    let radius = usable / 2.0;

    // 图形到中心的最远距离；外圆正好贴着这条边，所以约等于半径。
    let cx = (x0 + x1) / 2.0;
    let cy = (y0 + y1) / 2.0;
    let mut farthest = 0.0f32;
    for contour in CONTOURS {
        for (x, y) in contour.iter() {
            let dx = *x as f32 - cx;
            let dy = *y as f32 - cy;
            farthest = farthest.max((dx * dx + dy * dy).sqrt());
        }
    }
    (radius / farthest).min(1.0)
}

#[cfg(test)]
mod tests {
    use super::*;

    const ALL: [IconState; 5] = [
        IconState::Ok,
        IconState::Error,
        IconState::Maintenance,
        IconState::Restarting,
        IconState::Down,
    ];

    fn alpha(state: IconState) -> Vec<u8> {
        rgba(state).chunks_exact(4).map(|p| p[3]).collect()
    }

    #[test]
    fn every_state_draws_the_same_shape() {
        // 形状恒定是「只换颜色」的前提：所有状态的覆盖度必须逐像素相同。
        let base = alpha(IconState::Ok);
        for st in ALL {
            assert_eq!(alpha(st), base, "{st:?} 的形状与 Ok 不一致");
        }
    }

    #[test]
    fn states_differ_only_in_color() {
        let colors: Vec<[u8; 3]> = ALL.map(color).to_vec();
        for (i, a) in colors.iter().enumerate() {
            for b in colors.iter().skip(i + 1) {
                assert_ne!(a, b, "两种状态用了同一个颜色，无法区分");
            }
        }
        // 像素只用两种颜色：状态色（主体）与该状态的描边色。描边是形状的
        // 一部分，同一状态内恒定，所以状态之间的差异仍只体现在颜色上。
        for st in ALL {
            let want = color(st);
            let edge = rim(st);
            assert_ne!(want, edge, "{st:?} 的主体与描边同色，图形会糊成一块");
            let mut core = 0;
            for px in rgba(st).chunks_exact(4) {
                if px[3] == 0 {
                    continue;
                }
                let rgb = &px[..3];
                assert!(
                    rgb == want || rgb == edge,
                    "{st:?} 出现了意外的颜色 {rgb:?}"
                );
                if rgb == want {
                    core += 1;
                }
            }
            assert!(core > 200, "{st:?} 的主体色只有 {core} 个像素");
        }
    }

    #[test]
    fn center_petal_is_filled_solid() {
        // 中心花瓣在品牌原图里是中空的尖叶，托盘尺寸下会缩成一个小洞，
        // 像根管子。这里约定它必须是实心的。
        //
        // 判据：取图形中心的一块竖向窄带（正好覆盖中心花瓣所在的横向区间），
        // 统计这块区域的历史填充率。空心时中轴附近会有大片透明，填充率低；
        // 实心时几乎被墨迹填满。用区域而非单列取样，避免采样点恰好落在
        // 花瓣之间的缝隙或描边上而产生误判。
        let a = alpha(IconState::Ok);
        let size = SIZE as usize;

        // 中心花瓣横向约占图形宽度的 18%，这里取更窄的一段避开左右花瓣。
        let half_w = size / 14;
        let cx = size / 2;

        // 纵向取图形中段，避开外圈圆环所在的顶部与底部。
        let from = size * 3 / 10;
        let to = size * 7 / 10;

        let mut inked = 0usize;
        let mut total = 0usize;
        for y in from..to {
            for x in (cx - half_w)..=(cx + half_w) {
                total += 1;
                if a[y * size + x] > 128 {
                    inked += 1;
                }
            }
        }

        let ratio = inked as f32 / total as f32;
        assert!(
            ratio > 0.90,
            "中心花瓣填充率只有 {ratio:.3}，说明花心又被挖空了"
        );
    }

    #[test]
    fn icon_has_ink_and_empty_areas() {
        // 品牌图形有实有虚：既要有完全实心的笔画，也要有完全透明的背景，
        // 否则说明光栅化把图形填死了或整块丢了。
        let a = alpha(IconState::Ok);
        assert!(a.iter().any(|&v| v == 255), "没有实心像素");
        assert!(a.iter().any(|&v| v == 0), "没有透明像素");
        let inked = a.iter().filter(|&&v| v > 0).count();
        let ratio = inked as f32 / a.len() as f32;
        // 描边会让墨迹比纯主体略胖，上限相应放宽。
        assert!((0.15..0.72).contains(&ratio), "填色比例 {ratio:.2} 不合理");
    }

    #[test]
    fn shape_is_centered_and_fills_the_tile() {
        // 裁掉留白后图形应当居中且铺满，四周至少留一点余量。
        let a = alpha(IconState::Ok);
        let size = SIZE as usize;
        let (mut min_x, mut max_x, mut min_y, mut max_y) = (size, 0, size, 0);
        for y in 0..size {
            for x in 0..size {
                if a[y * size + x] > 0 {
                    min_x = min_x.min(x);
                    max_x = max_x.max(x);
                    min_y = min_y.min(y);
                    max_y = max_y.max(y);
                }
            }
        }
        let w = max_x - min_x + 1;
        let h = max_y - min_y + 1;
        assert!(w as f32 / size as f32 > 0.8, "横向只占 {w}/{size}");
        assert!(h as f32 / size as f32 > 0.8, "纵向只占 {h}/{size}");
        let off_x = (min_x + max_x) as i32 - (size as i32 - 1);
        let off_y = (min_y + max_y) as i32 - (size as i32 - 1);
        assert!(off_x.abs() <= 2 && off_y.abs() <= 2, "没居中：{off_x},{off_y}");
    }

    #[test]
    fn zero_degrees_matches_static_icon() {
        // 静止状态必须与旋转 0 度完全一致，否则动效停下时图标会「抖一下」。
        for st in ALL {
            assert_eq!(rgba_at(st, 0.0), rgba(st), "{st:?} 的 0 度与静止不一致");
        }
    }

    #[test]
    fn spin_frames_cover_full_turn_and_loop() {
        assert_eq!(spin_frames(0), 0.0);
        assert_eq!(spin_frames(SPIN_FRAMES), 0.0, "整圈应回到起点");
        let angles: Vec<f32> = (0..SPIN_FRAMES).map(spin_frames).collect();
        for w in angles.windows(2) {
            assert!(w[1] > w[0], "角度必须递增：{} -> {}", w[0], w[1]);
        }
        assert!(angles[SPIN_FRAMES as usize - 1] < 360.0);
    }

    #[test]
    fn rotating_keeps_ink_inside_the_tile() {
        // 旋转补偿的意义：任何角度下图形都不能被位图边缘裁掉。
        // 描边本身会贴到边距里，所以判据是「最外一圈不是实心」，
        // 也就是确实留出了边距，而不是图形被切齐。
        let size = SIZE as usize;
        for frame in 0..SPIN_FRAMES {
            let px = rgba_at(IconState::Restarting, spin_frames(frame));
            let a: Vec<u8> = px.chunks_exact(4).map(|p| p[3]).collect();
            for i in 0..size {
                for idx in [i, (size - 1) * size + i, i * size, i * size + size - 1] {
                    assert!(a[idx] < 250, "第 {frame} 帧边缘第 {i} 点被裁：{}", a[idx]);
                }
            }
        }
    }

    #[test]
    fn rotating_keeps_ink_and_preserves_area() {
        // 旋转只应改变形状朝向，不该把图形吃掉或放大。
        let base = rgba_at(IconState::Ok, 0.0);
        let base_ink = base.chunks_exact(4).map(|p| p[3] as u32).sum::<u32>();
        for frame in 0..SPIN_FRAMES {
            let deg = spin_frames(frame);
            let px = rgba_at(IconState::Ok, deg);
            let ink = px.chunks_exact(4).map(|p| p[3] as u32).sum::<u32>();
            assert!(ink > 0, "{deg} 度时图形消失了");
            let ratio = ink as f32 / base_ink.max(1) as f32;
            assert!((0.75..1.05).contains(&ratio), "{deg} 度时覆盖量变成 {ratio:.2}");
        }
    }

    #[test]
    fn rotating_actually_changes_the_pixels() {
        // 45 度与 0 度必须明显不同，否则说明旋转参数没生效。
        let a = rgba_at(IconState::Restarting, 0.0);
        let b = rgba_at(IconState::Restarting, 45.0);
        let diff = a
            .chunks_exact(4)
            .zip(b.chunks_exact(4))
            .filter(|(x, y)| (x[3] as i16 - y[3] as i16).abs() > 32)
            .count();
        assert!(diff > (SIZE * SIZE / 10) as usize, "只有 {diff} 个像素不同");
    }

    #[test]
    fn spin_frame_sequence_is_a_closed_loop() {
        // 动画靠取模循环，最后一帧之后必须回到第 0 帧，否则会跳。
        let frames: Vec<u32> = (0..SPIN_FRAMES * 2).map(|i| i % SPIN_FRAMES).collect();
        assert_eq!(frames[0], 0);
        assert_eq!(frames[SPIN_FRAMES as usize], 0);
        // 角度序列必须单调递增后才归零，中间不能出现倒退。
        let mut prev = -1.0;
        for (i, f) in frames.iter().enumerate() {
            let deg = spin_frames(*f);
            if i > 0 && deg == 0.0 {
                assert!(prev > 300.0, "归零前角度只有 {prev}");
            } else if i > 0 {
                assert!(deg > prev, "角度倒退：{prev} -> {deg}");
            }
            prev = deg;
        }
    }

    #[test]
    fn prerendered_frames_are_all_distinct() {
        // 预渲染整圈帧：每帧都必须互不相同，否则动画会看起来卡住。
        let mut seen = std::collections::HashSet::new();
        for frame in 0..SPIN_FRAMES {
            let px = rgba_at(IconState::Restarting, spin_frames(frame));
            let key: u64 = px
                .chunks_exact(4)
                .enumerate()
                .fold(0u64, |acc, (i, p)| acc.wrapping_add((p[3] as u64) << (i % 61)));
            assert!(seen.insert(key), "第 {frame} 帧与前面某帧完全相同");
        }
        assert_eq!(seen.len(), SPIN_FRAMES as usize);
    }
}
