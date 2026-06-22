const http  = require('http');
const https = require('https');
const fs    = require('fs');
const path  = require('path');
const nodemailer = require('nodemailer');

const PORT = 4000;
const STATIC_DIR = __dirname;
const TO_EMAIL = 'kyunghwa.yoon@atto-research.com';

// ── SMTP 설정 ──────────────────────────────────────────────
// 실제 발송 시 아래 설정을 채워주세요.
// const transporter = nodemailer.createTransport({
//   host: 'smtp.your-provider.com',
//   port: 587,
//   secure: false,
//   auth: { user: 'YOUR_USER', pass: 'YOUR_PASS' },
// });

// 개발용: Ethereal 테스트 계정 (실제 발송 없음, 콘솔에 미리보기 URL 출력)
let transporter;
nodemailer.createTestAccount().then(account => {
  transporter = nodemailer.createTransport({
    host: 'smtp.ethereal.email',
    port: 587,
    auth: { user: account.user, pass: account.pass },
  });
  console.log('📧 테스트 메일 계정 준비 완료 (Ethereal)');
});

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.css':  'text/css',
  '.js':   'application/javascript',
  '.svg':  'image/svg+xml',
  '.png':  'image/png',
  '.jpg':  'image/jpeg',
  '.ico':  'image/x-icon',
};

const server = http.createServer(async (req, res) => {
  // GET /api/pricing  — proxy to alrouter.ai (avoids browser CORS)
  if (req.method === 'GET' && req.url === '/api/pricing') {
    https.get('https://alrouter.ai/api/pricing', upstream => {
      res.writeHead(upstream.statusCode, {
        'Content-Type': 'application/json',
        'Access-Control-Allow-Origin': '*',
      });
      upstream.pipe(res);
    }).on('error', () => {
      res.writeHead(502, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ success: false, data: [] }));
    });
    return;
  }

  // POST /api/contact
  if (req.method === 'POST' && req.url === '/api/contact') {
    let body = '';
    req.on('data', c => body += c);
    req.on('end', async () => {
      try {
        const { org, email, phone, scale_type, headcount, input_tokens, output_tokens, features, notes } = JSON.parse(body);
        const featureList = Array.isArray(features) && features.length ? features.join(', ') : '미선택';
        const scaleText = scale_type === 'token'
          ? `입력 ${input_tokens || 0}M / 출력 ${output_tokens || 0}M 토큰/월`
          : (headcount ? headcount + '명' : '미입력');
        const mailOptions = {
          from: `"AlRouter 문의" <noreply@alrouter.io>`,
          to: TO_EMAIL,
          replyTo: email,
          subject: `[기업 도입 문의] ${org}`,
          text: [
            `기관명: ${org}`,
            `이메일: ${email}`,
            `전화번호: ${phone || '미입력'}`,
            `도입 규모: ${scaleText}`,
            `필요 기능: ${featureList}`,
            '',
            `[기타 의견]`,
            notes || '없음',
          ].join('\n'),
          html: `
            <h2 style="color:#0EA5E9">기업 도입 문의</h2>
            <table style="border-collapse:collapse;font-size:14px">
              <tr><td style="padding:6px 12px;font-weight:bold">기관명</td><td style="padding:6px 12px">${org}</td></tr>
              <tr><td style="padding:6px 12px;font-weight:bold">이메일</td><td style="padding:6px 12px">${email}</td></tr>
              <tr><td style="padding:6px 12px;font-weight:bold">전화번호</td><td style="padding:6px 12px">${phone || '미입력'}</td></tr>
              <tr><td style="padding:6px 12px;font-weight:bold">도입 규모</td><td style="padding:6px 12px">${scaleText}</td></tr>
              <tr><td style="padding:6px 12px;font-weight:bold">필요 기능</td><td style="padding:6px 12px">${featureList}</td></tr>
            </table>
            <h3>기타 의견</h3>
            <p style="white-space:pre-wrap">${notes || '없음'}</p>
          `,
        };

        const info = await transporter.sendMail(mailOptions);
        const previewUrl = nodemailer.getTestMessageUrl(info);

        console.log(`\n✉️  문의 접수: ${org} <${email}>`);
        if (previewUrl) console.log(`   미리보기: ${previewUrl}`);

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ ok: true }));
      } catch (err) {
        console.error('메일 발송 오류:', err.message);
        res.writeHead(500, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ ok: false }));
      }
    });
    return;
  }

  // Static file serving
  const urlPath = req.url.split('?')[0];
  let filePath = path.join(STATIC_DIR, urlPath === '/' ? 'index.html' : urlPath);
  if (!fs.existsSync(filePath)) {
    res.writeHead(404); res.end('Not found'); return;
  }
  const ext = path.extname(filePath);
  res.writeHead(200, { 'Content-Type': MIME[ext] || 'application/octet-stream' });
  fs.createReadStream(filePath).pipe(res);
});

server.listen(PORT, () => {
  console.log(`\n🚀 AlRouter 홈 서버 실행 중`);
  console.log(`   http://localhost:${PORT}         (한국어)`);
  console.log(`   http://localhost:${PORT}/index_en.html  (영문)`);
  console.log(`\n   Ctrl+C 로 종료\n`);
});
