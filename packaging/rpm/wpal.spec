%global wpal_user wpal
%global wpal_group wpal
%global wpal_home /var/lib/wpal
%global wpal_etc /etc/wpal

Name:           wpal
Version:        %{version}
Release:        1%{?dist}
Summary:        轻量级跨平台服务器资源监控看板

License:        Apache-2.0
URL:            https://github.com/xuhongteng/wpal
Source0:        %{name}_%{version}_%{os}_%{arch}.tar.gz

BuildArch:      %{_arch}
Requires:       shadow-utils
Requires(pre):  systemd

%description
Wpal 是一款轻量级、跨平台的服务器资源监控看板。
它以单个静态二进制交付，内嵌 Web 看板，通过 WebSocket 实时推送指标，
历史数据保存在有界的环形文件里，阈值告警直接呈现在看板上。

%prep
%setup -q

%install
mkdir -p %{buildroot}%{_bindir}
install -m 0755 wpal %{buildroot}%{_bindir}/wpal

mkdir -p %{buildroot}%{_unitdir}
install -m 0644 systemd/wpal.service %{buildroot}%{_unitdir}/wpal.service

mkdir -p %{buildroot}%{_sysconfdir}/wpal
install -m 0644 debian/config.yaml %{buildroot}%{_sysconfdir}/wpal/config.yaml

mkdir -p %{buildroot}%{_localstatedir}/wpal

%files
%license LICENSE
%doc README.md README.en.md CHANGELOG.md NOTICE
%{_bindir}/wpal
%{_unitdir}/wpal.service
%config(noreplace) %{_sysconfdir}/wpal/config.yaml
%attr(0755,wpal,wpal) %{_localstatedir}/wpal

%pre
getent group %{wpal_group} >/dev/null || groupadd -r %{wpal_group}
getent passwd %{wpal_user} >/dev/null || \
    useradd -r -g %{wpal_group} -d %{wpal_home} -s /sbin/nologin \
            -c "wpal monitoring" %{wpal_user}
exit 0

%post
%systemd_post wpal.service
exit 0

%preun
%systemd_preun wpal.service
exit 0

%postun
%systemd_postun_with_restart wpal.service
exit 0

%changelog
* Thu Jan 01 2026 Wpal Contributors <maintainers@wpal.dev> - 1.0.0-1
- Initial release
