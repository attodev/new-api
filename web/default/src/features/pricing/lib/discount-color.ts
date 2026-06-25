// 할인율(percent)에 따른 배지 색상 클래스 (heatmap: 높을수록 빨강).
export function discountBadgeClasses(percent: number): string {
  if (percent >= 50) return 'bg-red-500/15 text-red-600 dark:text-red-400'
  if (percent >= 30) return 'bg-orange-500/15 text-orange-600 dark:text-orange-400'
  if (percent >= 15) return 'bg-amber-500/15 text-amber-600 dark:text-amber-400'
  return 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400'
}
