const http = require('http');
const fs   = require('fs');
const path = require('path');
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
  // POST /api/contact
  if (req.method === 'POST' && req.url === '/api/contact') {
    let body = '';
    req.on('data', c => body += c);
    req.on('end', async () => {
      try {
        const { org, email, phone, message } = JSON.parse(body);
        const mailOptions = {
          from: `"AlRouter 문의" <noreply@alrouter.io>`,
          to: TO_EMAIL,
          replyTo: email,
          subject: `[기업 도입 문의] ${org}`,
          text: [
            `기관명: ${org}`,
            `이메일: ${email}`,
            `전화번호: ${phone || '미입력'}`,
            '',
            `[문의사항]`,
            message,
          ].join('\n'),
          html: `
            <h2 style="color:#0EA5E9">기업 도입 문의</h2>
            <table style="border-collapse:collapse;font-size:14px">
              <tr><td style="padding:6px 12px;font-weight:bold">기관명</td><td style="padding:6px 12px">${org}</td></tr>
              <tr><td style="padding:6px 12px;font-weight:bold">이메일</td><td style="padding:6px 12px">${email}</td></tr>
              <tr><td style="padding:6px 12px;font-weight:bold">전화번호</td><td style="padding:6px 12px">${phone || '미입력'}</td></tr>
            </table>
            <h3>문의사항</h3>
            <p style="white-space:pre-wrap">${message}</p>
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
  let filePath = path.join(STATIC_DIR, req.url === '/' ? 'index.html' : req.url);
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
