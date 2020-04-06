<?php

namespace Friendo;

class Frontend
{

    public function init ()
    {
        $frontend = new self;
        $frontend->loadAssets();
        $frontend->loadData();
        $frontend->hideAdminToolbar();
    }

    protected function hideAdminToolbar ()
    {
        add_filter('show_admin_bar', '__return_false');
    }

    protected function loadAssets ()
    {
        add_action('init', [$this, 'loadScripts']);
        add_action('init', [$this, 'loadStyles']);
    }

    protected function loadData ()
    {
        add_action('wp', [$this, 'localizeDataArray']);
    }

    protected function getData ()
    {
        return [
            'user' => get_current_user_id(),
            'object' => get_queried_object_id(),
            'root' => esc_url_raw( rest_url() ),
            'nonce' => wp_create_nonce( 'wp_rest' )
        ];
    }

    final public function loadScripts () {
        wp_enqueue_script('friendo-js', FRIENDO_PLUGIN_URI . 'assets/dist/app.bundle.js', null, '0.0.1', true);
    }

    final public function loadStyles () {
        wp_enqueue_style('friendo-css', FRIENDO_PLUGIN_URI . 'assets/dist/app.bundle.css', [], '0.0.1', 'all');
    }

    final public function localizeDataArray () {
        wp_localize_script('friendo-js', 'friendoData', $this->getData() );
    }
}