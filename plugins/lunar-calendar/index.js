'use strict';

const ZODIAC_BY_BRANCH = {
  '子': '鼠', '丑': '牛', '寅': '虎', '卯': '兔', '辰': '龙', '巳': '蛇',
  '午': '马', '未': '羊', '申': '猴', '酉': '鸡', '戌': '狗', '亥': '猪',
};
const CHINESE_DIGITS = ['〇', '一', '二', '三', '四', '五', '六', '七', '八', '九'];

function partsMap(parts) {
  return Object.fromEntries(parts.map(part => [part.type, part.value]));
}

function shanghaiGregorianDate(date) {
  const parts = partsMap(new Intl.DateTimeFormat('en', {
    timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
  }).formatToParts(date));
  return `${parts.year}-${parts.month}-${parts.day}`;
}

function validDate(date) {
  if (typeof date !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(date)) {
    throw new TypeError('date must use YYYY-MM-DD format');
  }
  const [year, month, day] = date.split('-').map(Number);
  const parsed = new Date(Date.UTC(year, month - 1, day, 12));
  if (parsed.getUTCFullYear() !== year || parsed.getUTCMonth() + 1 !== month || parsed.getUTCDate() !== day) {
    throw new RangeError('date is not a valid Gregorian calendar date');
  }
  return parsed;
}

function chineseDay(day) {
  if (day < 1 || day > 30) throw new RangeError('lunar day is outside the supported range');
  if (day === 10) return '初十';
  if (day === 20) return '二十';
  if (day === 30) return '三十';
  if (day < 10) return `初${CHINESE_DIGITS[day]}`;
  if (day < 20) return `十${CHINESE_DIGITS[day % 10]}`;
  if (day < 30) return `廿${CHINESE_DIGITS[day % 10]}`;
  return '三十';
}

function lookup(date) {
  const gregorianDate = date || shanghaiGregorianDate(new Date());
  const instant = validDate(gregorianDate);
  const parts = partsMap(new Intl.DateTimeFormat('zh-CN-u-ca-chinese', {
    calendar: 'chinese', timeZone: 'Asia/Shanghai', year: 'numeric', month: 'long', day: 'numeric',
  }).formatToParts(instant));
  const relatedYear = Number(parts.relatedYear);
  const lunarDay = Number(parts.day);
  const yearName = parts.yearName || '';
  if (!Number.isInteger(relatedYear) || !parts.month || !Number.isInteger(lunarDay) || yearName.length < 2) {
    throw new Error('当前运行环境缺少 ICU 中国农历数据');
  }
  const branch = yearName.at(-1);
  const zodiac = ZODIAC_BY_BRANCH[branch];
  if (!zodiac) throw new Error('当前运行环境返回了无法识别的干支年');
  const lunarDate = `${yearName}年${parts.month}${chineseDay(lunarDay)}`;
  return {
    gregorianDate,
    lunarYear: relatedYear,
    lunarMonth: parts.month,
    isLeapMonth: parts.month.startsWith('闰'),
    lunarDay,
    lunarDayName: chineseDay(lunarDay),
    ganzhiYear: yearName,
    zodiac,
    lunarDate,
    timeZone: 'Asia/Shanghai',
  };
}

module.exports = {
  name: 'lunar-calendar',
  apply(ctx) {
    ctx.tool({
      name: 'get_lunar_date',
      description: '查询今天或一个公历日期对应的中国农历日期，返回农历年月日、闰月标记、干支年和生肖。默认按中国标准时间（Asia/Shanghai）确定今天。',
      parameters: {
        type: 'object',
        properties: {
          date: { type: 'string', description: '可选公历日期，格式 YYYY-MM-DD；省略时查询中国标准时间的今天。' },
        },
      },
      handler(args = {}) {
        return lookup(typeof args.date === 'string' && args.date.trim() ? args.date.trim() : undefined);
      },
    });
  },
  _lookup: lookup,
};
